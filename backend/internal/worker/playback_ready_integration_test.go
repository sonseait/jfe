package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
)

func TestPlaybackReadyBeforeBitrateMeasurement(t *testing.T) {
	t.Run("legacy HLS", func(t *testing.T) { testPlaybackReadyBeforeBitrateMeasurement(t, false) })
	t.Run("fragmented MP4", func(t *testing.T) { testPlaybackReadyBeforeBitrateMeasurement(t, true) })
}
func testPlaybackReadyBeforeBitrateMeasurement(t *testing.T, mp4 bool) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires isolated integration schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := config.Config{MediaRoot: t.TempDir(), CacheRoot: t.TempDir()}
	path := filepath.Join(cfg.MediaRoot, "video.mp4")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	for _, stmt := range []string{
		`INSERT INTO users(id,username,password_hash,role) VALUES ($1,$1,'unused','admin')`,
		`INSERT INTO libraries(id,name,kind,paths) VALUES ($1,'Ready test','movies','{}')`,
		`INSERT INTO items(id,library_id,kind,title,sort_title) VALUES ($1,$1,'movie','Ready','ready')`,
	} {
		if _, err := pool.Exec(ctx, stmt, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_files(id,item_id,path,size,modified_at) VALUES ($1,$1,$2,0,0)`, id, path); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO playback_sessions(id,user_id,item_id,file_id,token_hash,method,state,expires_at) VALUES ($1,$1,$1,$1,'unused','remux','preparing',now()+interval '1 hour')`, id); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	release := filepath.Join(bin, "release")
	t.Setenv("JFE_TEST_PROBE_RELEASE", release)
	t.Setenv("PATH", bin)
	scripts := map[string]string{
		"ffmpeg":  "#!/bin/sh\nprintf '#EXTM3U\\n#EXTINF:1,\\nsegment-000000.ts\\n#EXTINF:1,\\nsegment-000001.ts\\n' > index.m3u8\nprintf sample > segment-000000.ts\nprintf sample > segment-000001.ts\n",
		"ffprobe": "#!/bin/sh\nwhile [ ! -e \"$JFE_TEST_PROBE_RELEASE\" ]; do /bin/sleep 0.05; done\nprintf '{\"streams\":[],\"packets\":[],\"format\":{\"bit_rate\":\"123456\"}}'\n",
	}
	payload := []byte(`{"AudioIndex":-1,"SubtitleIndex":-1}`)
	if mp4 {
		payload = []byte(`{"AudioIndex":-1,"SubtitleIndex":-1,"decision":{"container":"mp4","audioAction":"copy"}}`)
		scripts["ffmpeg"] = "#!/bin/sh\nprintf '\\000\\000\\000\\010moof\\000\\000\\000\\014mdatdata' > stream.mp4\nwhile [ ! -e \"$JFE_TEST_PROBE_RELEASE\" ]; do /bin/sleep 0.05; done\n"
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(pool)
	w := Worker{DB: db, Pool: pool, Config: cfg}
	done := make(chan error, 1)
	go func() {
		done <- w.transcode(ctx, store.Job{ResourceID: id, Payload: payload})
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	for {
		p, err := db.GetPlayback(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if p.State == "ready" {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("worker finished before ready: %v", err)
		case <-ctx.Done():
			t.Fatal("bitrate probe blocked playback readiness")
		case <-time.After(20 * time.Millisecond):
		}
	}
	readInfo := func() media.StreamInfo {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(cfg.CacheRoot, "playback", id, "stream-info.json"))
		if err != nil {
			t.Fatal(err)
		}
		var info media.StreamInfo
		if err := json.Unmarshal(data, &info); err != nil {
			t.Fatal(err)
		}
		return info
	}
	if info := readInfo(); info.BitrateSource != "pending" || info.VideoTranscoded {
		t.Fatalf("initial info: %+v", info)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("measurement did not complete")
	}
	if info := readInfo(); info.BitrateSource != "segment" || info.TotalBitrate != 123456 {
		t.Fatalf("final info: %+v", info)
	}
	if err := os.Remove(release); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET state='preparing' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() {
		stopped <- w.transcode(ctx, store.Job{ResourceID: id, Payload: payload})
		close(stopped)
	}()
	defer func() { cancel(); <-stopped }()
	for {
		p, err := db.GetPlayback(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if p.State == "ready" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second session not ready")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := db.StopPlayback(ctx, store.StopPlaybackParams{ID: id, UserID: id}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopped session waited for bitrate probe")
	}
	if _, err := os.Stat(filepath.Join(cfg.CacheRoot, "playback", id)); !os.IsNotExist(err) {
		t.Fatalf("stopped session output was not removed: %v", err)
	}
	var logs bytes.Buffer
	previousLogger := log.Logger
	log.Logger = zerolog.New(&logs)
	defer func() { log.Logger = previousLogger }()
	run := func(kind string) (string, string) {
		t.Helper()
		j, err := db.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "transcoder", Kind: kind, ResourceID: id, Payload: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		j.LeaseID = uuid.NewString()
		if _, err := pool.Exec(ctx, `UPDATE jobs SET state='running',lease_id=$2 WHERE id=$1`, j.ID, j.LeaseID); err != nil {
			t.Fatal(err)
		}
		w.runJob(ctx, j)
		var state, message string
		if err := pool.QueryRow(ctx, `SELECT state,error FROM jobs WHERE id=$1`, j.ID).Scan(&state, &message); err != nil {
			t.Fatal(err)
		}
		return state, message
	}
	if state, message := run("playback"); state != "cancelled" || message != "" {
		t.Fatalf("stopped job: %s %q", state, message)
	}
	if strings.Contains(logs.String(), `"level":"error"`) || !strings.Contains(logs.String(), `"message":"job cancelled"`) {
		t.Fatalf("cancellation logs: %s", logs.String())
	}
	logs.Reset()
	if state, message := run("invalid-kind"); state != "failed" || message == "" {
		t.Fatalf("real failure: %s %q", state, message)
	}
	if !strings.Contains(logs.String(), `"level":"error"`) || !strings.Contains(logs.String(), `"message":"job failed"`) {
		t.Fatalf("failure not logged: %s", logs.String())
	}
}

package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
)

func TestEncodingFailsClosed(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires isolated integration schema")
	}
	ctx := context.Background()
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
		`INSERT INTO libraries(id,name,kind,paths) VALUES ($1,'Encoding test','movies','{}')`,
		`INSERT INTO items(id,library_id,kind,title,sort_title) VALUES ($1,$1,'movie','Test','test')`,
	} {
		if _, err := pool.Exec(ctx, stmt, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_files(id,item_id,path,size,modified_at) VALUES ($1,$1,$2,0,0)`, id, path); err != nil {
		t.Fatal(err)
	}
	db := store.New(pool)
	defer func() {
		_ = db.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: []byte(`{"mode":"disabled","maxConcurrent":1}`)})
	}()
	bin := t.TempDir()
	invocations := filepath.Join(bin, "invocations")
	// Simulate a driver failure and record every attempt to detect CPU retries.
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\necho \"$*\" >> \"$JFE_TEST_INVOCATIONS\"\necho 'NVENC driver unavailable' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("JFE_TEST_INVOCATIONS", invocations)
	w := Worker{DB: db, Pool: pool, Config: cfg}
	for _, mode := range []string{"disabled", "nvidia"} {
		t.Run(mode, func(t *testing.T) {
			if err := db.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: []byte(`{"mode":"` + mode + `"}`)}); err != nil {
				t.Fatal(err)
			}
			pid := uuid.NewString()
			if _, err := pool.Exec(ctx, `INSERT INTO playback_sessions(id,user_id,item_id,file_id,token_hash,method,state,expires_at) VALUES ($1,$2,$2,$2,'unused','transcode','preparing',now()+interval '1 hour')`, pid, id); err != nil {
				t.Fatal(err)
			}
			if err := w.transcode(ctx, store.Job{ResourceID: pid, Payload: []byte(`{"AudioIndex":-1,"SubtitleIndex":-1}`)}); err == nil {
				t.Fatal("expected encoding failure")
			}
			p, err := db.GetPlayback(ctx, pid)
			if err != nil || p.State != "failed" {
				t.Fatalf("failed playback: %+v %v", p, err)
			}
			data, err := os.ReadFile(invocations)
			if mode == "disabled" {
				if !os.IsNotExist(err) {
					t.Fatal("disabled queued job invoked FFmpeg")
				}
			} else if err != nil || strings.Count(string(data), "\n") != 1 || !strings.Contains(string(data), "-c:v h264_nvenc") {
				t.Fatalf("unexpected encoder attempts: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(cfg.CacheRoot, "playback", pid)); !os.IsNotExist(err) {
				t.Fatal("failed playback cache retained")
			}
		})
	}
}

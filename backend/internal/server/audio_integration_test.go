package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"jfe/backend/internal/worker"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAudioAPIPlaybackTagsAndPermissions(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool, e := pgxpool.New(ctx, base)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	cfg := config.Config{MediaRoot: root, CacheRoot: t.TempDir(), ImportRoot: t.TempDir(), Python: os.Getenv("JFE_PYTHON"), Concurrency: 1}
	s := New(cfg, pool)
	q := s.DB
	userID, lib := uuid.NewString(), uuid.NewString()
	u, e := q.CreateUser(ctx, store.CreateUserParams{ID: userID, Username: userID, Role: "user", PasswordHash: "unused"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", userID)
	_, e = q.SaveLibrary(ctx, store.SaveLibraryParams{ID: lib, Name: "Music", Kind: "music", Paths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DELETE FROM libraries WHERE id=$1", lib)
	login, e := s.login(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "track.m4a")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "4", "-c:a", "aac", path).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	title, album := "Audio title", "Album"
	if _, e = audio.Helper(ctx, cfg.Python, "write", path, &audio.Patch{Title: &title, Album: &album}); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = worker.Run(ctx, cfg, pool, "scanner") }()
	defer func() { cancel(); <-done }()
	if _, e = q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "scan", ResourceID: lib, Payload: []byte("{}")}); e != nil {
		t.Fatal(e)
	}
	var files []store.MediaFile
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		files, e = q.LibraryFiles(ctx, lib)
		if e == nil && len(files) > 0 {
			break
		}
	}
	if len(files) != 1 {
		t.Fatal("audio scan did not index fixture", e)
	}
	f := files[0]
	call := func(method, path string, body, target any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+login.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, e := s.App.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 500 {
			t.Fatalf("%s %s: %s", method, path, data)
		}
		if target != nil {
			if e = json.Unmarshal(data, target); e != nil {
				t.Fatalf("%s", data)
			}
		}
		return resp.StatusCode
	}
	var tags FileTagsDTO
	if status := call("GET", "/api/v1/files/"+f.ID+"/tags", nil, nil); status != 403 {
		t.Fatal("ungranted tags", status)
	}
	if e = q.GrantLibrary(ctx, store.GrantLibraryParams{UserID: userID, LibraryID: lib}); e != nil {
		t.Fatal(e)
	}
	if status := call("GET", "/api/v1/files/"+f.ID+"/tags", nil, &tags); status != 200 || tags.Writable {
		t.Fatal("read-only tags", status, tags)
	}
	changed := "Edited from API"
	edit := TagChanges{Items: []TagChange{{FileID: f.ID, Fingerprint: tags.Fingerprint, Patch: AudioTagPatch{Title: &changed}}}}
	if status := call("POST", "/api/v1/audio/tags", edit, nil); status != 403 {
		t.Fatal("read access allowed write", status)
	}
	if e = q.GrantImport(ctx, store.GrantImportParams{UserID: userID, LibraryID: lib}); e != nil {
		t.Fatal(e)
	}
	var jobs JobsDTO
	if status := call("POST", "/api/v1/audio/tags", edit, &jobs); status != 200 || len(jobs.Items) != 1 {
		t.Fatal(status, jobs)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		j, e := q.GetJob(ctx, jobs.Items[0].ID)
		if e == nil && j.State == "completed" {
			break
		}
	}
	read, e := audio.Helper(ctx, cfg.Python, "read", path, nil)
	if e != nil || read.Tags.Title != changed {
		t.Fatal("file was not tagged", read, e)
	}
	if status := call("POST", "/api/v1/audio/tags", edit, nil); status != 409 {
		t.Fatal("stale tag request", status)
	}
	var playback PlaybackDTO
	if status := call("POST", "/api/v1/playback", PlaybackRequest{FileID: f.ID, DirectPlay: true, AudioIndex: -1, SubtitleIndex: -1}, &playback); status != 200 || playback.Method != "direct" {
		t.Fatal("audio direct", status, playback)
	}
	req := httptest.NewRequest("GET", playback.URL+"?token="+playback.StreamToken, nil)
	req.Header.Set("Range", "bytes=0-99")
	resp, e := s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 206 {
		t.Fatal("audio range", resp.StatusCode)
	}
	if status := call("POST", "/api/v1/playback", PlaybackRequest{FileID: f.ID, AudioIndex: -1, SubtitleIndex: -1}, &playback); status != 200 || playback.Method != "audio" {
		t.Fatal("audio fallback with video disabled", status, playback)
	}
	if status := call("POST", "/api/v1/imports", ImportRequest{LibraryID: lib, URL: "https://localhost/internal"}, nil); status != 422 {
		t.Fatal("unsafe import URL", status)
	}
	var source ImportSourceDTO
	if status := call("POST", "/api/v1/imports", ImportRequest{LibraryID: lib, URL: "https://youtu.be/abcdefghijk"}, &source); status != 200 {
		t.Fatal("preview", status)
	}
	if e = q.ClearAccess(ctx, userID); e != nil {
		t.Fatal(e)
	}
	if status := call("POST", "/api/v1/import-sources/"+source.ID+"/download", nil, nil); status != 403 {
		t.Fatal("revoked import accepted", status)
	}
	req = httptest.NewRequest("GET", playback.URL+"?token="+playback.StreamToken, nil)
	resp, e = s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("revoked stream", resp.StatusCode)
	}
}

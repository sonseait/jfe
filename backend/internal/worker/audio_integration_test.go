package worker

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAudioScanTagWriteAndRecovery(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable integration schema")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, base)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q := store.New(pool)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	cache := t.TempDir()
	python := os.Getenv("JFE_PYTHON")
	if python == "" {
		python = "python3"
	}
	if e = exec.Command(python, "-c", "import mutagen").Run(); e != nil {
		t.Fatal("integration requires Mutagen via JFE_PYTHON")
	}
	library, user := uuid.NewString(), uuid.NewString()
	_, e = q.CreateUser(ctx, store.CreateUserParams{ID: user, Username: user, PasswordHash: "test", Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user)
	l, e := q.SaveLibrary(ctx, store.SaveLibraryParams{ID: library, Name: "Music", Kind: "music", Paths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM libraries WHERE id=$1", library)
	path := filepath.Join(root, "track.flac")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:a", "flac", path).CombinedOutput(); e != nil {
		t.Fatalf("%v: %s", e, b)
	}
	title, album := "Original", "Album"
	artists := []string{"Artist"}
	if _, e = audio.Helper(ctx, python, "write", path, &audio.Patch{Title: &title, Album: &album, Artists: &artists}); e != nil {
		t.Fatal(e)
	}
	w := Worker{Config: config.Config{MediaRoot: root, CacheRoot: cache, Python: python}, DB: q, Pool: pool, Role: "scanner"}
	if e = w.scan(ctx, l.ID, ScanOptions{}, func(int, int) error { return nil }); e != nil {
		t.Fatal(e)
	}
	files, e := q.LibraryFiles(ctx, library)
	if e != nil || len(files) != 1 {
		t.Fatalf("files=%v %v", files, e)
	}
	f := files[0]
	before, e := audioDigest(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.SaveProgress(ctx, store.SaveProgressParams{UserID: user, ItemID: f.ItemID, Position: 0.4}); e != nil {
		t.Fatal(e)
	}
	changed := "Edited"
	fingerprint, _ := audio.Fingerprint(path)
	payload, _ := json.Marshal(audio.Patch{Title: &changed})
	j, e := q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "audio_tags", ResourceID: f.ID, Payload: []byte("{}")})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM jobs WHERE id=$1", j.ID)
	j.LeaseID = uuid.NewString()
	j.LeaseUntil = time.Now().Add(time.Minute)
	if _, e = pool.Exec(ctx, "UPDATE jobs SET state='running',lease_id=$2,lease_until=$3 WHERE id=$1", j.ID, j.LeaseID, j.LeaseUntil); e != nil {
		t.Fatal(e)
	}
	if e = q.SaveTagJob(ctx, store.SaveTagJobParams{ID: j.ID, UserID: user, FileID: f.ID, Fingerprint: fingerprint, Patch: payload}); e != nil {
		t.Fatal(e)
	}
	if e = w.writeAudioTags(ctx, j); e != nil {
		t.Fatal(e)
	}
	after, e := audioDigest(ctx, path)
	if e != nil || before != after {
		t.Fatal("audio changed", e)
	}
	r, e := audio.Helper(ctx, python, "read", path, nil)
	if e != nil || r.Tags.Title != changed || r.Tags.Album != album {
		t.Fatalf("tags %+v %v", r, e)
	}
	// Simulate recovery after publish but before recording indexed phase.
	published, _ := audio.Fingerprint(path)
	if e = os.WriteFile(filepath.Join(root, ".jfe-tag-"+j.ID+".json"), []byte(published), 0600); e != nil {
		t.Fatal(e)
	}
	if e = q.SetTagPhase(ctx, store.SetTagPhaseParams{ID: j.ID, Phase: "prepared"}); e != nil {
		t.Fatal(e)
	}
	if e = w.writeAudioTags(ctx, j); e != nil {
		t.Fatal("recovery", e)
	}
	if e = w.scan(ctx, l.ID, ScanOptions{}, func(int, int) error { return nil }); e != nil {
		t.Fatal(e)
	}
	updated, e := q.GetItem(ctx, f.ItemID)
	if e != nil || updated.Title != changed {
		t.Fatal("rescan lost edit", updated, e)
	}
	state, e := q.GetState(ctx, store.GetStateParams{UserID: user, ItemID: f.ItemID})
	if e != nil || state.Position != 0.4 {
		t.Fatal("progress lost", state, e)
	}
	// A stale fingerprint must not replace a newer on-disk edit.
	if e = q.SetTagPhase(ctx, store.SetTagPhaseParams{ID: j.ID, Phase: "pending"}); e != nil {
		t.Fatal(e)
	}
	if e = w.writeAudioTags(ctx, j); e == nil {
		t.Fatal("accepted stale fingerprint")
	}
}
func TestDownloaderRecoveryAndPermission(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable integration schema")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, base)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q := store.New(pool)
	user, library := uuid.NewString(), uuid.NewString()
	_, e = q.CreateUser(ctx, store.CreateUserParams{ID: user, Username: user, PasswordHash: "x", Role: "user"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user)
	_, e = q.SaveLibrary(ctx, store.SaveLibraryParams{ID: library, Name: "Music", Kind: "music", Paths: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM libraries WHERE id=$1", library)
	w := Worker{DB: q, Pool: pool, Role: "downloader"}
	if e = w.importPermission(ctx, user, library); e == nil {
		t.Fatal("ungranted import")
	}
	if e = q.GrantLibrary(ctx, store.GrantLibraryParams{UserID: user, LibraryID: library}); e != nil {
		t.Fatal(e)
	}
	if e = w.importPermission(ctx, user, library); e == nil {
		t.Fatal("read implies write")
	}
	if e = q.GrantImport(ctx, store.GrantImportParams{UserID: user, LibraryID: library}); e != nil {
		t.Fatal(e)
	}
	source, e := q.CreateImportSource(ctx, store.CreateImportSourceParams{ID: uuid.NewString(), UserID: user, LibraryID: library, Url: "https://www.youtube.com/watch?v=abcdefghijk"})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.sourceAllowed(ctx, source); e != nil {
		t.Fatal(e)
	}
	j, e := q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: "youtube_preview", ResourceID: source.ID, Payload: []byte("{}")})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM jobs WHERE id=$1", j.ID)
	claimed, e := w.claim(ctx)
	if e != nil || claimed.ID != j.ID {
		t.Fatal(claimed, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1", j.ID); e != nil {
		t.Fatal(e)
	}
	reclaimed, e := w.claim(ctx)
	if e != nil || reclaimed.ID != j.ID || reclaimed.LeaseID == claimed.LeaseID {
		t.Fatal(reclaimed, e)
	}
	if e = w.validLease(ctx, claimed); e == nil {
		t.Fatal("old downloader can publish")
	}
	if e = q.ClearAccess(ctx, user); e != nil {
		t.Fatal(e)
	}
	if e = w.sourceAllowed(ctx, source); e == nil {
		t.Fatal("revocation ignored")
	}
}

func TestAudioOnlyHLSWithVideoDisabled(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable integration schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, base)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q := store.New(pool)
	root := t.TempDir()
	path := filepath.Join(root, "audio.flac")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=330", "-t", "5", "-c:a", "flac", path).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	id := uuid.NewString()
	for _, query := range []string{`INSERT INTO users(id,username,password_hash,role) VALUES($1,$1,'x','admin')`, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Audio','music','{}')`, `INSERT INTO items(id,library_id,kind,title,sort_title) VALUES($1,$1,'track','Audio','audio')`} {
		if _, e = pool.Exec(ctx, query, id); e != nil {
			t.Fatal(e)
		}
	}
	defer pool.Exec(context.Background(), "DELETE FROM libraries WHERE id=$1", id)
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", id)
	if e = q.SaveFile(ctx, store.SaveFileParams{ID: id, ItemID: id, Path: path, Duration: 5, Probe: []byte("{}")}); e != nil {
		t.Fatal(e)
	}
	if e = q.StartPlayback(ctx, store.StartPlaybackParams{ID: id, UserID: id, ItemID: id, FileID: id, TokenHash: "x", Method: "audio", State: "preparing", ExpiresAt: time.Now().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	cache := t.TempDir()
	w := Worker{DB: q, Pool: pool, Config: config.Config{MediaRoot: root, CacheRoot: cache}}
	if e = w.transcode(ctx, store.Job{ResourceID: id, Payload: []byte(`{"AudioIndex":-1,"SubtitleIndex":-1}`)}); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(cache, "playback", id, "stream-info.json"))
	if e != nil {
		t.Fatal(e)
	}
	var info struct {
		VideoTranscoded bool
		AudioCodec      string
	}
	if e = json.Unmarshal(data, &info); e != nil {
		t.Fatal(e)
	}
	if info.VideoTranscoded || info.AudioCodec != "aac" {
		t.Fatalf("unexpected output %s", data)
	}
}

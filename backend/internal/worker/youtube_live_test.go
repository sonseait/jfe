package worker

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"os"
	"testing"
	"time"
)

// Opt-in only: deterministic suites must not depend on a remote provider.
func TestYouTubeLiveImport(t *testing.T) {
	url := os.Getenv("JFE_TEST_YOUTUBE_URL")
	if url == "" {
		t.Skip("set an accessible YouTube test video URL")
	}
	database := os.Getenv("JFE_TEST_SCHEMA_URL")
	if database == "" {
		t.Fatal("disposable migrated schema is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool, e := pgxpool.New(ctx, database)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q := store.New(pool)
	user, library, sourceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, e = q.CreateUser(ctx, store.CreateUserParams{ID: user, Username: user, PasswordHash: "test", Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user)
	root := t.TempDir()
	l, e := q.SaveLibrary(ctx, store.SaveLibraryParams{ID: library, Name: "Live test", Kind: "music", Paths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DELETE FROM libraries WHERE id=$1", library)
	source, e := q.CreateImportSource(ctx, store.CreateImportSourceParams{ID: sourceID, LibraryID: library, UserID: user, Url: url})
	if e != nil {
		t.Fatal(e)
	}
	w := Worker{DB: q, Pool: pool, Role: "downloader", Config: config.Config{MediaRoot: root, ImportRoot: t.TempDir(), CacheRoot: t.TempDir(), Python: "python3"}}
	for _, kind := range []string{"youtube_preview", "youtube_download"} {
		j, e := q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: kind, ResourceID: source.ID, Payload: []byte("{}")})
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Exec(context.Background(), "DELETE FROM jobs WHERE id=$1", j.ID)
		j.LeaseID = uuid.NewString()
		if _, e = pool.Exec(ctx, "UPDATE jobs SET state='running',lease_id=$2,lease_until=now()+interval '5 minutes' WHERE id=$1", j.ID, j.LeaseID); e != nil {
			t.Fatal(e)
		}
		if e = w.youtubeJob(ctx, j); e != nil {
			t.Fatal(kind, e)
		}
	}
	if e = w.scanAudio(ctx, l, func(int, int) error { return nil }); e != nil {
		t.Fatal(e)
	}
	files, e := q.LibraryFiles(ctx, library)
	if e != nil || len(files) != 1 {
		t.Fatal("expected one playable imported file", len(files), e)
	}
	if files[0].Duration <= 0 {
		t.Fatal("missing audio duration")
	}
}

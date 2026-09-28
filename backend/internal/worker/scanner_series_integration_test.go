package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
)

func TestScanRegroupsSeriesByFolder(t *testing.T) {
	url := os.Getenv("JFE_TEST_SCHEMA_URL")
	if url == "" {
		t.Skip("requires isolated integration schema")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Series','series',$2)`, id, []string{root}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, id)
	defer pool.Exec(ctx, `DELETE FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE library_id=$1)`, id)
	db := store.New(pool)
	legacy := stable(id, "series", "old title")
	if err = db.UpsertItem(ctx, store.UpsertItemParams{ID: legacy, LibraryID: id, Kind: "series", Title: "Old Title"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"Show/S01/ep1.mp4", "Show/02/Unrelated.S01E02.mp4", "Other/Season 01/Old.Title.S01E01.mp4"} {
		path := filepath.Join(root, rel)
		if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte("cached fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		itemID := stable(id, path)
		if err = db.UpsertItem(ctx, store.UpsertItemParams{ID: itemID, LibraryID: id, Kind: "episode", ParentID: legacy, Title: rel}); err != nil {
			t.Fatal(err)
		}
		if err = db.SaveFile(ctx, store.SaveFileParams{ID: stable("file", id, path), ItemID: itemID, Path: path, Size: stat.Size(), ModifiedAt: stat.ModTime().UnixNano(), Probe: []byte(`{"probe_version":1,"format":{"duration":"60"}}`)}); err != nil {
			t.Fatal(err)
		}
	}
	w := Worker{DB: db, Pool: pool, Config: config.Config{MediaRoot: root, CacheRoot: t.TempDir()}}
	for i := 0; i < 2; i++ {
		if err = w.scan(ctx, id, ScanOptions{}, func(int, int) error { return nil }); err != nil {
			t.Fatal(err)
		}
		var shows, episodes, old int
		if err = pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='series'), count(*) FILTER(WHERE kind='episode'), count(*) FILTER(WHERE id=$2) FROM items WHERE library_id=$1`, id, legacy).Scan(&shows, &episodes, &old); err != nil {
			t.Fatal(err)
		}
		if shows != 2 || episodes != 3 || old != 0 {
			t.Fatalf("shows=%d episodes=%d old=%d", shows, episodes, old)
		}
		item, err := db.GetItem(ctx, stable(id, filepath.Join(root, "Show/02/Unrelated.S01E02.mp4")))
		if err != nil || item.ParentID != stable(id, "series-folder", filepath.Join(root, "Show")) || item.Season != 2 || item.Episode != 2 {
			t.Fatalf("wrong regrouped episode: %+v %v", item, err)
		}
	}
	// Force refresh must schedule once per show, including already identified
	// shows, while respecting locks and never scheduling episode TMDB lookups.
	if _, err := pool.Exec(ctx, `UPDATE items SET provider_id='42',metadata_locked=(title='Other') WHERE library_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	w.Config.TMDBToken = "test-token"
	for _, force := range []bool{false, true, true} {
		if err := w.scan(ctx, id, ScanOptions{ForceMetadata: force}, func(int, int) error { return nil }); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs j JOIN items i ON i.id=j.resource_id WHERE i.library_id=$1 AND j.kind='metadata'`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if force {
			want = 1
		}
		if count != want {
			t.Fatalf("force=%v: got %d metadata jobs, want %d", force, count, want)
		}
	}
	if err := db.SaveSetting(ctx, store.SaveSettingParams{Key: "general", Value: []byte(`{"autoMetadata":false}`)}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM settings WHERE key='general'`)
	if _, err := pool.Exec(ctx, `DELETE FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE library_id=$1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET provider_id='' WHERE library_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		if err := w.scan(ctx, id, ScanOptions{ForceMetadata: force}, func(int, int) error { return nil }); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE library_id=$1)`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if force {
			want = 1
		}
		if count != want {
			t.Fatalf("automatic metadata disabled: force=%v jobs=%d", force, count)
		}
	}
}

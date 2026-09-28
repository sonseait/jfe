package server

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
	"os"
	"testing"
)

func TestLibraryCatalogShowsSeriesRoots(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires isolated integration schema")
	}
	ctx := route.WithUser(context.Background(), route.Principal{ID: uuid.NewString(), Role: "admin"})
	pool, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := New(config.Config{}, pool)
	library, showA, showB := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Shows','series','{}')`, library); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, library) }()
	for _, item := range []struct{ id, parent, kind, title string }{
		{showA, "", "series", "Alpha"}, {showB, "", "series", "Beta"},
		{uuid.NewString(), showA, "episode", "A first episode"},
		{uuid.NewString(), showA, "episode", "B second episode"},
		{uuid.NewString(), showB, "episode", "C episode"},
	} {
		err := s.DB.UpsertItem(ctx, store.UpsertItemParams{ID: item.id, LibraryID: library, ParentID: item.parent, Kind: item.kind, Title: item.title, SortTitle: item.title})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.catalog(ctx, CatalogQuery{LibraryID: library, Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != showA || !first.HasMore {
		t.Fatalf("first root page: %+v %v", first, err)
	}
	second, err := s.catalog(ctx, CatalogQuery{LibraryID: library, Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != showB || second.HasMore {
		t.Fatalf("second root page: %+v %v", second, err)
	}
	episodes, err := s.catalog(ctx, CatalogQuery{LibraryID: library, ParentID: showA})
	if err != nil || len(episodes.Items) != 2 {
		t.Fatalf("show episodes: %+v %v", episodes, err)
	}
	for _, item := range episodes.Items {
		if item.Kind != "episode" || item.ParentID != showA {
			t.Fatalf("wrong child: %+v", item)
		}
	}
	explicit, err := s.catalog(ctx, CatalogQuery{LibraryID: library, Kind: "episode"})
	if err != nil || len(explicit.Items) != 3 {
		t.Fatalf("explicit episode listing: %+v %v", explicit, err)
	}
	hidden, err := s.catalog(route.WithUser(ctx, route.Principal{ID: uuid.NewString(), Role: "user"}), CatalogQuery{LibraryID: library})
	if err != nil || len(hidden.Items) != 0 {
		t.Fatalf("library permission bypassed: %+v %v", hidden, err)
	}
}

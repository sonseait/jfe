package server

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/route"
)

func TestResumeCatalogRecentEpisodesAndPagination(t *testing.T) {
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
	user, other, library := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{user, other} {
		if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,password_hash,role) VALUES($1,$1,'unused','user')", id); err != nil {
			t.Fatal(err)
		}
		defer func(id string) { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", id) }(id)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Shows','series','{}')", library); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM libraries WHERE id=$1", library) }()
	if _, err := pool.Exec(ctx, "INSERT INTO library_access(user_id,library_id) VALUES($1,$2)", user, library); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for n := 0; n < 10; n++ {
		id := uuid.NewString()
		ids = append(ids, id)
		if _, err := pool.Exec(ctx, "INSERT INTO items(id,library_id,parent_id,kind,title,sort_title) VALUES($1,$2,'show','episode',$3,$3)", id, library, fmt.Sprintf("Episode %02d", n)); err != nil {
			t.Fatal(err)
		}
		// All but the latest episode share a timestamp to exercise cursor tie handling.
		if _, err := pool.Exec(ctx, "INSERT INTO user_state(user_id,item_id,position,updated_at) VALUES($1,$2,30,'2026-01-01'::timestamptz + $3 * interval '1 day')", user, id, n/9); err != nil {
			t.Fatal(err)
		}
	}
	s := New(config.Config{}, pool)
	viewer := route.WithUser(ctx, route.Principal{ID: user, Role: "user"})
	first, err := s.catalog(viewer, CatalogQuery{Resume: true, LibraryID: library, Limit: 8})
	if err != nil || len(first.Items) != 8 || first.Items[0].ID != ids[9] || !first.HasMore {
		t.Fatalf("recent episode missing from first page: %+v %v", first, err)
	}
	second, err := s.catalog(viewer, CatalogQuery{Resume: true, LibraryID: library, Limit: 8, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 2 || second.HasMore {
		t.Fatalf("second page: %+v %v", second, err)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ID] {
			t.Fatalf("duplicate resume item %s", item.ID)
		}
		seen[item.ID] = true
	}
	hidden, err := s.catalog(route.WithUser(ctx, route.Principal{ID: other, Role: "user"}), CatalogQuery{Resume: true})
	if err != nil || len(hidden.Items) != 0 {
		t.Fatalf("resume permission bypass: %+v %v", hidden, err)
	}
	empty, err := s.catalog(route.WithUser(ctx, route.Principal{ID: other, Role: "admin"}), CatalogQuery{Resume: true})
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("another user's progress leaked: %+v %v", empty, err)
	}
}

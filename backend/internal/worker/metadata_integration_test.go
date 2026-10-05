package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAutomaticMetadataMatch(t *testing.T) {
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
	id := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES ($1,'Matching','movies','{}')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,kind,title,sort_title,year) VALUES ($1,$1,'movie','Dune','dune',2021)`, id); err != nil {
		t.Fatal(err)
	}
	db := store.New(pool)
	if err := db.SaveSetting(ctx, store.SaveSettingParams{Key: "general", Value: []byte(`{"metadataLanguage":"vi-VN","autoMetadata":false}`)}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM settings WHERE key='general'`)
	w := Worker{DB: db, Pool: pool, Role: "scanner", Config: config.Config{TMDBToken: "test-token", CacheRoot: t.TempDir()}}
	var portrait bytes.Buffer
	if err := jpeg.Encode(&portrait, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	ambiguous, lookups := false, 0
	portraits := 0
	http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "image.tmdb.org" && r.URL.Path == "/t/p/w185/actor.jpg" {
			portraits++
			if r.Header.Get("Authorization") != "" {
				t.Fatal("token sent to image host")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(portrait.Bytes())), Header: http.Header{}}, nil
		}
		if r.URL.Host != "api.themoviedb.org" {
			return nil, errors.New("unexpected host")
		}
		if r.URL.Query().Get("language") != "vi-VN" {
			t.Fatal("metadata language not applied")
		}
		body := ""
		switch r.URL.Path {
		case "/3/search/movie":
			if r.URL.Query().Get("query") != "Dune" {
				return nil, errors.New("wrong query")
			}
			if ambiguous {
				body = `{"results":[{"id":1,"title":"Dune","release_date":"2021-01-01"},{"id":2,"title":"Dune","release_date":"2021-02-01"}]}`
			} else {
				body = `{"results":[{"id":1,"title":"Dune","release_date":"1984-01-01"},{"id":2,"title":"Dune","release_date":"2021-01-01"}]}`
			}
		case "/3/movie/2":
			if r.URL.Query().Get("append_to_response") != "credits" {
				t.Fatal("cast credits were not requested")
			}
			lookups++
			body = `{"title":"Dune","release_date":"2021-01-01","overview":"Correct remake","credits":{"cast":[{"id":42,"name":"Actor","character":"Paul","profile_path":"/actor.jpg"}]}}`
		default:
			return nil, errors.New("wrong metadata candidate requested")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	if err := w.metadata(ctx, store.Job{ResourceID: id, Payload: []byte(`{"automatic":true}`)}); err != nil || lookups != 0 {
		t.Fatal("disabled automatic lookup ran")
	}
	if err := w.metadata(ctx, store.Job{ResourceID: id, Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	item, err := db.GetItem(ctx, id)
	if err != nil || item.ProviderID != "2" || item.Overview != "Correct remake" || lookups != 1 {
		t.Fatalf("wrong automatic match: %+v %v", item, err)
	}
	var cast []media.CastMember
	expected := media.NewCastMember("Actor", "Paul", 42)
	expected.Image = "cast-" + expected.ID + ".jpg"
	if err := json.Unmarshal(item.CastMembers, &cast); err != nil || len(cast) != 1 || cast[0] != expected {
		t.Fatalf("TMDB cast missing: %s", item.CastMembers)
	}
	if data, err := os.ReadFile(filepath.Join(w.Config.CacheRoot, "artwork", expected.Image)); err != nil || len(data) == 0 {
		t.Fatalf("portrait not cached: %v", err)
	}
	if err := db.PatchSetting(ctx, store.PatchSettingParams{Key: "general", Value: []byte(`{"castImages":false}`)}); err != nil {
		t.Fatal(err)
	}
	if err := w.metadata(ctx, store.Job{ResourceID: id, Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	item, err = db.GetItem(ctx, id)
	if err != nil || json.Unmarshal(item.CastMembers, &cast) != nil || len(cast) != 1 || cast[0].Image != expected.Image || portraits != 1 || lookups != 2 {
		t.Fatal("disabled portraits downloaded or discarded cached image")
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET provider_id='' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	ambiguous = true
	j, err := db.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "metadata", ResourceID: id, Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Only claim this test job; other worker tests may leave leases in the schema.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='running',lease_id='matching-test',attempts=1 WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	j.LeaseID, j.Attempts = "matching-test", 1
	w.runJob(ctx, j)
	var state, message string
	if err := pool.QueryRow(ctx, `SELECT state,error FROM jobs WHERE id=$1`, j.ID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	item, err = db.GetItem(ctx, id)
	if err != nil || state != "failed" || message != media.ErrNeedsIdentification.Error() || item.ProviderID != "" || item.Overview != "Correct remake" || lookups != 2 {
		t.Fatalf("ambiguous match changed metadata or retried: %s %s %+v", state, message, item)
	}
}

func TestEpisodeMetadataUsesIdentifiedSeries(t *testing.T) {
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
	libraryID, seriesID, episodeID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Episodes','series','{}')`, libraryID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, libraryID)
	if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,kind,title,sort_title,provider_id) VALUES($1,$2,'series','A Show','a show','42')`, seriesID, libraryID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,kind,title,sort_title,season,episode) VALUES($1,$2,$3,'episode','S02E03','s02e03',2,3)`, episodeID, libraryID, seriesID); err != nil {
		t.Fatal(err)
	}
	db := store.New(pool)
	if err = db.SaveSetting(ctx, store.SaveSettingParams{Key: "general", Value: []byte(`{"metadataLanguage":"vi-VN"}`)}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM settings WHERE key='general'`)
	w := Worker{DB: db, Pool: pool, Config: config.Config{TMDBToken: "test-token", CacheRoot: t.TempDir()}}
	var artwork bytes.Buffer
	if err = jpeg.Encode(&artwork, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "image.tmdb.org" && r.URL.Path == "/t/p/w500/still.jpg" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(artwork.Bytes())), Header: http.Header{}}, nil
		}
		if r.URL.Host != "api.themoviedb.org" || r.URL.Path != "/3/tv/42/season/2/episode/3" {
			return nil, errors.New("wrong episode metadata request")
		}
		if r.URL.Query().Get("language") == "en-US" {
			body := `{"name":"A Real Episode Title"}`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		}
		if r.URL.Query().Get("append_to_response") != "credits" || r.URL.Query().Get("language") != "vi-VN" {
			t.Fatal("episode metadata request omitted options")
		}
		body := `{"id":303,"name":"Episode 3","air_date":"2024-02-03","overview":"Episode overview","still_path":"/still.jpg","credits":{"cast":[]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	if err = w.metadata(ctx, store.Job{ResourceID: episodeID, Payload: []byte(`{"automatic":true}`)}); err != nil {
		t.Fatal(err)
	}
	episode, err := db.GetItem(ctx, episodeID)
	if err != nil || episode.Title != "A Real Episode Title" || episode.Year != 2024 || episode.Overview != "Episode overview" || episode.ProviderID != "303" || episode.Poster != episodeID+".jpg" {
		t.Fatalf("episode metadata was not saved: %+v %v", episode, err)
	}
	if data, err := os.ReadFile(filepath.Join(w.Config.CacheRoot, "artwork", episode.Poster)); err != nil || len(data) == 0 {
		t.Fatalf("episode still was not cached: %v", err)
	}
}

func TestSeriesMetadataSchedulesEpisodes(t *testing.T) {
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
	libraryID, seriesID := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Schedule episodes','series','{}')`, libraryID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, libraryID)
	defer pool.Exec(ctx, `DELETE FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE library_id=$1)`, libraryID)
	if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,kind,title,sort_title) VALUES($1,$2,'series','A Show','a show')`, seriesID, libraryID); err != nil {
		t.Fatal(err)
	}
	for _, episode := range []struct {
		locked   bool
		provider string
	}{{false, ""}, {true, ""}, {false, "123"}} {
		if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,kind,title,sort_title,season,episode,metadata_locked,provider_id) VALUES($1,$2,$3,'episode','Episode','episode',1,$4,$5,$6)`, uuid.NewString(), libraryID, seriesID, 1, episode.locked, episode.provider); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(pool)
	w := Worker{DB: db, Pool: pool, Config: config.Config{TMDBToken: "test-token", CacheRoot: t.TempDir()}}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.themoviedb.org" || r.URL.Path != "/3/tv/42" {
			return nil, errors.New("wrong series metadata request")
		}
		body := `{"id":42,"name":"A Show","first_air_date":"2024-01-01","credits":{"cast":[]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	if err = w.metadata(ctx, store.Job{ResourceID: seriesID, Payload: []byte(`{"providerId":42,"mode":"replace"}`)}); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='metadata' AND resource_id IN (SELECT id FROM items WHERE parent_id=$1)`, seriesID).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("episode jobs=%d err=%v", jobs, err)
	}
}

func TestMissingEpisodeMetadataPreservesExisting(t *testing.T) {
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
	libraryID, seriesID := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Missing episodes','series','{}')`, libraryID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, libraryID)
	defer pool.Exec(ctx, `DELETE FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE library_id=$1)`, libraryID)
	if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,kind,title,sort_title,provider_id,metadata_locked) VALUES($1,$2,'series','Keep series','keep series','42',true)`, seriesID, libraryID); err != nil {
		t.Fatal(err)
	}
	missingID, existingID, lockedID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, episode := range []struct {
		id, provider string
		locked       bool
	}{{missingID, "", false}, {existingID, "123", false}, {lockedID, "", true}} {
		if _, err = pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,kind,title,sort_title,provider_id,metadata_locked,season,episode) VALUES($1,$2,$3,'episode','Keep episode','keep episode',$4,$5,1,1)`, episode.id, libraryID, seriesID, episode.provider, episode.locked); err != nil {
			t.Fatal(err)
		}
	}
	w := Worker{DB: store.New(pool), Pool: pool, Config: config.Config{TMDBToken: "test-token", CacheRoot: t.TempDir()}}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path != "/3/tv/42/season/1/episode/1" {
			t.Fatalf("unexpected lookup: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":456,"name":"Fetched episode","overview":"New metadata"}`)), Header: http.Header{}}, nil
	})
	if err = w.metadata(ctx, store.Job{ResourceID: seriesID, Payload: []byte(`{"mode":"missing"}`)}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE resource_id IN (SELECT id FROM items WHERE parent_id=$1)`, seriesID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("jobs=%d err=%v", count, err)
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM jobs WHERE resource_id=$1`, missingID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatal("missing refresh fetched series metadata")
	}
	for _, id := range []string{existingID, lockedID, missingID, missingID} {
		if err = w.metadata(ctx, store.Job{ResourceID: id, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 {
		t.Fatalf("existing or locked metadata overwritten: requests=%d", requests)
	}
	for _, id := range []string{seriesID, existingID, lockedID} {
		item, err := w.DB.GetItem(ctx, id)
		if err != nil || !strings.HasPrefix(item.Title, "Keep ") {
			t.Fatalf("metadata changed: %+v %v", item, err)
		}
	}
	item, err := w.DB.GetItem(ctx, missingID)
	if err != nil || item.ProviderID != "456" || item.Title != "Fetched episode" {
		t.Fatalf("missing episode not populated: %+v %v", item, err)
	}
}

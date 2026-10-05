package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

func TestAutomaticPlaybackBitrateBudget(t *testing.T) {
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
	root := t.TempDir()
	s := New(config.Config{MediaRoot: root}, pool)
	user, library, item, file := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,password_hash,role) VALUES($1,$1,'unused','admin')", user); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user) }()
	if _, err := pool.Exec(ctx, "INSERT INTO libraries(id,name,kind,paths) VALUES($1,'Films','movies',$2)", library, []string{root}); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM libraries WHERE id=$1", library) }()
	if err := s.DB.UpsertItem(ctx, store.UpsertItemParams{ID: item, LibraryID: library, Kind: "movie", Title: "Film", SortTitle: "film"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "movie.mp4")
	if err := os.WriteFile(path, []byte("negotiation fixture; never executed by a worker"), 0600); err != nil {
		t.Fatal(err)
	}
	probe := []byte(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":3840,"height":2160}],"format":{"format_name":"mp4"}}`)
	if err := s.DB.SaveFile(ctx, store.SaveFileParams{ID: file, ItemID: item, Path: path, Size: 3000000000, Duration: 1000, Probe: probe}); err != nil {
		t.Fatal(err)
	}
	before, err := s.DB.GetSetting(ctx, "encoding")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.DB.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: before}) }()
	viewer := route.WithUser(ctx, route.Principal{ID: user, Role: "admin"})
	for _, tc := range []struct {
		name                string
		auto                bool
		requested, expected int
	}{
		{"automatic lower budget", true, 500000, 500000},
		{"automatic server ceiling", true, 50000000, 4000000},
		{"manual server profile", false, 500000, 4000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.DB.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: []byte(`{"mode":"nvidia","bitrate720":4000000}`)}); err != nil {
				t.Fatal(err)
			}
			playback, err := s.startPlayback(viewer, PlaybackRequest{FileID: file, AutoQuality: tc.auto, MaxHeight: 720, MaxBitrate: tc.requested, AudioIndex: -1, SubtitleIndex: -1, Position: 120})
			if err != nil || playback.Method != "transcode" || playback.Position != 120 {
				t.Fatalf("negotiation: %+v %v", playback, err)
			}
			var payload []byte
			if err := pool.QueryRow(ctx, "SELECT payload FROM jobs WHERE resource_id=$1 AND kind='playback'", playback.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var request PlaybackRequest
			if err := json.Unmarshal(payload, &request); err != nil || request.MaxBitrate != tc.expected || request.AutoQuality != tc.auto {
				t.Fatalf("queued bitrate policy: %s %v", payload, err)
			}
		})
	}

	t.Run("HDR decision persists and queues conversion", func(t *testing.T) {
		hdrProbe := []byte(`{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","profile":"Main 10","pix_fmt":"yuv420p10le","color_transfer":"smpte2084","color_primaries":"bt2020","color_space":"bt2020nc","width":3840,"height":2160}],"format":{"format_name":"mp4"}}`)
		if err := s.DB.SaveFile(ctx, store.SaveFileParams{ID: file, ItemID: item, Path: path, Size: 3000000000, Duration: 1000, Probe: hdrProbe}); err != nil {
			t.Fatal(err)
		}
		playback, err := s.startPlayback(viewer, PlaybackRequest{FileID: file, DirectPlay: true, AudioIndex: -1, SubtitleIndex: -1, Capabilities: &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Video: []VideoCapabilityDTO{}, Audio: []string{"aac"}}})
		if err != nil || playback.Method != "transcode" || playback.Decision == nil || !playback.Decision.ToneMapped {
			t.Fatalf("HDR negotiation %+v %v", playback, err)
		}
		var payload []byte
		if err = pool.QueryRow(ctx, "SELECT payload FROM jobs WHERE resource_id=$1", playback.ID).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var queued struct{ Decision PlaybackDecisionDTO }
		if err = json.Unmarshal(payload, &queued); err != nil || !queued.Decision.ToneMapped {
			t.Fatalf("HDR job %s %v", payload, err)
		}
		var cached []byte
		if err = pool.QueryRow(ctx, "SELECT decision FROM playback_sessions WHERE id=$1", playback.ID).Scan(&cached); err != nil {
			t.Fatal(err)
		}
		var decision PlaybackDecisionDTO
		if err = json.Unmarshal(cached, &decision); err != nil || !decision.ToneMapped {
			t.Fatalf("stored HDR decision %s %v", cached, err)
		}
	})
	if err := s.DB.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: []byte(`{"mode":"disabled"}`)}); err != nil {
		t.Fatal(err)
	}
	_, err = s.startPlayback(viewer, PlaybackRequest{FileID: file, AutoQuality: true, MaxHeight: 720, MaxBitrate: 500000, AudioIndex: -1, SubtitleIndex: -1})
	if err == nil {
		t.Fatal("automatic mode bypassed disabled video encoding")
	}
}

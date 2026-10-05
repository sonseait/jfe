package worker

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the real worker boundary while capturing GPU arguments. Pixel
// conversion itself is tested separately with real FFmpeg-generated HDR media.
func TestHDRWorkerPipeline(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := store.New(pool)
	before, err := db.GetSetting(ctx, "encoding")
	if err != nil {
		t.Fatal(err)
	}
	defer db.SaveSetting(context.Background(), store.SaveSettingParams{Key: "encoding", Value: before})
	if err = db.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: []byte(`{"mode":"nvidia"}`)}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaRoot: t.TempDir(), CacheRoot: t.TempDir()}
	path := filepath.Join(cfg.MediaRoot, "hdr.mkv")
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	capture := filepath.Join(bin, "args")
	t.Setenv("JFE_HDR_ARGS_FILE", capture)
	t.Setenv("PATH", bin)
	scripts := map[string]string{
		"ffmpeg":  "#!/bin/sh\nfor arg do if [ \"$arg\" = '-filters' ]; then printf ' .S. zscale V->V\\n .S. tonemap V->V\\n ... sidedata V->V\\n ... limiter V->V\\n'; exit; fi; done\nfor last do :; done\ncase \"$last\" in *.ass) printf '[Script Info]\\n' > \"$last\"; exit;; esac\nprintf '%s\\n' \"$@\" > \"$JFE_HDR_ARGS_FILE\"\nprintf '#EXTM3U\\n#EXTINF:1,\\nsegment-000000.ts\\n' > index.m3u8\nprintf sample > segment-000000.ts\n",
		"ffprobe": "#!/bin/sh\nprintf '{\"streams\":[],\"packets\":[],\"format\":{\"bit_rate\":\"100000\"}}'\n",
	}
	for name, script := range scripts {
		if err = os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, method, subCodec string
		sub                    int
	}{
		{"HDR video conversion", "transcode", "", -1},
		{"HDR with ASS", "transcode", "ass", 1},
		{"HDR with bitmap", "transcode", "hdmv_pgs_subtitle", 1},
		{"HDR remux preserves bitstream", "remux", "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			for _, statement := range []string{`INSERT INTO users(id,username,password_hash,role) VALUES($1,$1,'unused','admin')`, `INSERT INTO libraries(id,name,kind,paths) VALUES($1,$1,'movies','{}')`, `INSERT INTO items(id,library_id,title,sort_title,kind) VALUES($1,$1,$1,$1,'movie')`} {
				if _, err = pool.Exec(ctx, statement, id); err != nil {
					t.Fatal(err)
				}
			}
			defer pool.Exec(context.Background(), "DELETE FROM libraries WHERE id=$1", id)
			defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", id)
			probe, _ := json.Marshal(map[string]any{"streams": []map[string]any{{"index": 0, "codec_type": "video", "codec_name": "hevc", "profile": "Main 10", "pix_fmt": "yuv420p10le", "color_transfer": "smpte2084", "color_primaries": "bt2020", "color_space": "bt2020nc"}, {"index": 1, "codec_type": "subtitle", "codec_name": tc.subCodec}}})
			if err = db.SaveFile(ctx, store.SaveFileParams{ID: id, ItemID: id, Path: path, Probe: probe}); err != nil {
				t.Fatal(err)
			}
			if err = db.StartPlayback(ctx, store.StartPlaybackParams{ID: id, UserID: id, ItemID: id, FileID: id, TokenHash: "unused", Method: tc.method, State: "preparing", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			// Legacy queued jobs also need tone mapping, even without a decision field.
			payload, _ := json.Marshal(map[string]any{"audioIndex": -1, "subtitleIndex": tc.sub, "maxHeight": 720})
			w := Worker{DB: db, Pool: pool, Config: cfg}
			if err = w.transcode(ctx, store.Job{ResourceID: id, Payload: payload}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			args := string(data)
			converted := tc.method == "transcode"
			if strings.Contains(args, "tonemap=tonemap=hable") != converted || strings.Contains(args, "h264_nvenc") != converted {
				t.Fatalf("unexpected encode/filter arguments: %s", args)
			}
			if converted {
				if !strings.Contains(args, "-color_trc\nbt709") {
					t.Fatal("SDR output not tagged")
				}
				if tc.subCodec == "ass" && strings.Index(args, "tonemap=") > strings.Index(args, "subtitles=") {
					t.Fatal("ASS colors tone mapped")
				}
				if tc.subCodec == "hdmv_pgs_subtitle" && !strings.Contains(args, "[sdr][sub]overlay") {
					t.Fatal("PGS not over SDR base")
				}
			}
			data, err = os.ReadFile(filepath.Join(cfg.CacheRoot, "playback", id, "stream-info.json"))
			if err != nil {
				t.Fatal(err)
			}
			var info struct{ ToneMapped bool }
			if err = json.Unmarshal(data, &info); err != nil || info.ToneMapped != converted {
				t.Fatalf("wrong conversion report %s %v", data, err)
			}
		})
	}
}

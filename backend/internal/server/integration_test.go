package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/jpeg"
	"io"
	"jfe/backend/internal/config"
	"jfe/backend/internal/worker"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeMediaLifecycle(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("run make test-integration to prepare an isolated database schema")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Config{DatabaseURL: base, MediaRoot: t.TempDir(), CacheRoot: t.TempDir(), Concurrency: 1}
	pool, e := pgxpool.New(ctx, cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(cfg, pool)
	call := func(method, path, token string, body any, target any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, e := s.App.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if target != nil {
			if e = json.Unmarshal(data, target); e != nil {
				t.Fatalf("decode %s: %s", path, data)
			}
		}
		if res.StatusCode >= 500 {
			t.Fatalf("%s: %s", path, data)
		}
		return res.StatusCode
	}
	var login LoginDTO
	if status := call("POST", "/api/v1/auth/setup", "", SetupRequest{"admin", "testing-password"}, &login); status != 200 {
		t.Fatalf("setup %d", status)
	}
	if call("POST", "/api/v1/auth/setup", "", SetupRequest{"other", "testing-password"}, nil) != 409 {
		t.Fatal("setup replay accepted")
	}
	var user UserDTO
	if call("POST", "/api/v1/admin/users", login.Token, UserRequest{Username: "viewer", Password: "testing-password", Role: "user", LibraryIDs: []string{}}, &user) != 200 {
		t.Fatal("create user")
	}
	var viewer LoginDTO
	call("POST", "/api/v1/auth/login", "", LoginRequest{"viewer", "testing-password"}, &viewer)
	if call("GET", "/api/v1/admin/users", viewer.Token, nil, nil) != 403 {
		t.Fatal("admin exposed")
	}
	var general GeneralSettingsDTO
	if call("GET", "/api/v1/admin/settings", "", nil, nil) != 401 || call("GET", "/api/v1/admin/settings", viewer.Token, nil, nil) != 403 {
		t.Fatal("settings read exposed")
	}
	if call("PATCH", "/api/v1/admin/settings", viewer.Token, map[string]any{"serverName": "hijacked"}, nil) != 403 {
		t.Fatal("settings write exposed")
	}
	if call("GET", "/api/v1/admin/settings", login.Token, nil, &general) != 200 || general.ServerName != "JFE" || general.WatchedPercent != 95 || general.TMDBConfigured {
		t.Fatalf("bad settings defaults: %+v", general)
	}
	if call("PATCH", "/api/v1/admin/settings", login.Token, map[string]any{"autoMetadata": false, "metadataLanguage": "vi-VN", "castImages": false}, &general) != 200 {
		t.Fatal("settings update failed")
	}
	if _, e := pool.Exec(ctx, `UPDATE settings SET value=value || '{"future":"keep"}'::jsonb WHERE key='general'`); e != nil {
		t.Fatal(e)
	}
	if call("PATCH", "/api/v1/admin/settings", login.Token, map[string]any{"serverName": " Cinema "}, &general) != 200 || general.ServerName != "Cinema" || general.AutoMetadata || general.CastImages || general.MetadataLanguage != "vi-VN" {
		t.Fatalf("partial update discarded settings: %+v", general)
	}
	var future string
	if e := pool.QueryRow(ctx, `SELECT value->>'future' FROM settings WHERE key='general'`).Scan(&future); e != nil || future != "keep" {
		t.Fatal("unknown setting lost")
	}
	var generalSystem SystemDTO
	if call("GET", "/api/v1/system", "", nil, &generalSystem) != 200 || generalSystem.Name != "Cinema" {
		t.Fatal("server name not applied")
	}
	for _, patch := range []any{map[string]any{"serverName": " "}, map[string]any{"metadataLanguage": "invalid"}, map[string]any{"watchedPercent": 49}, map[string]any{"watchedPercent": 101}} {
		if call("PATCH", "/api/v1/admin/settings", login.Token, patch, nil) != 422 {
			t.Fatal("invalid settings accepted")
		}
	}
	var library LibraryDTO
	if call("POST", "/api/v1/libraries", login.Token, LibraryRequest{Name: "Films", Kind: "movies", Paths: []string{cfg.MediaRoot}}, &library) != 200 {
		t.Fatal("create library")
	}
	if _, e = exec.LookPath("ffmpeg"); e != nil {
		t.Fatal("ffmpeg required")
	}
	mediaPath := filepath.Join(cfg.MediaRoot, "Fixture (2025).mp4")
	var portrait bytes.Buffer
	if e := jpeg.Encode(&portrait, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(cfg.MediaRoot, "actor.jpg"), portrait.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(cfg.MediaRoot, "Fixture (2025).nfo"), []byte(`<movie><actor><name>Test Actor</name><role>Lead</role><tmdbid>42</tmdbid><thumb>actor.jpg</thumb></actor></movie>`), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=160x90:rate=15", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", mediaPath)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("ffmpeg: %s %v", b, e)
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{}, 2)
	for _, role := range []string{"scanner", "transcoder"} {
		go func() { _ = worker.Run(workerCtx, cfg, pool, role); done <- struct{}{} }()
	}
	defer func() { stop(); <-done; <-done }()
	var job JobDTO
	if call("POST", "/api/v1/libraries/"+library.ID+"/scan?forceMetadata=invalid", login.Token, nil, nil) != 422 {
		t.Fatal("invalid scan option accepted")
	}
	if call("POST", "/api/v1/libraries/"+library.ID+"/scan?forceMetadata=true", viewer.Token, nil, nil) != 403 {
		t.Fatal("viewer forced a scan")
	}
	call("POST", "/api/v1/libraries/"+library.ID+"/scan?forceMetadata=true", login.Token, nil, &job)
	var forced bool
	if e := pool.QueryRow(ctx, `SELECT (payload->>'forceMetadata')::boolean FROM jobs WHERE id=$1`, job.ID).Scan(&forced); e != nil || !forced {
		t.Fatalf("force option not persisted: %v", e)
	}
	deadline := time.Now().Add(20 * time.Second)
	var items ItemsDTO
	for time.Now().Before(deadline) {
		call("GET", "/api/v1/items", login.Token, nil, &items)
		if len(items.Items) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(items.Items) != 1 {
		t.Fatal("scan did not create item")
	}
	var hidden ItemsDTO
	call("GET", "/api/v1/items", viewer.Token, nil, &hidden)
	if len(hidden.Items) != 0 {
		t.Fatal("library permissions leaked")
	}
	if call("GET", "/api/v1/items/"+items.Items[0].ID, viewer.Token, nil, nil) != 403 {
		t.Fatal("detail access leaked")
	}
	var detail DetailDTO
	call("GET", "/api/v1/items/"+items.Items[0].ID, login.Token, nil, &detail)
	if len(detail.Files) != 1 {
		t.Fatal("file missing")
	}
	// Scan completion includes local metadata persistence, which follows SaveFile.
	for len(detail.Cast) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		call("GET", "/api/v1/items/"+detail.Item.ID, login.Token, nil, &detail)
	}
	if len(detail.Cast) != 1 || detail.Cast[0].Name != "Test Actor" || detail.Cast[0].Character != "Lead" {
		t.Fatalf("local cast missing: %+v", detail.Cast)
	}
	if detail.Cast[0].Image == "" {
		t.Fatal("cast image missing")
	}
	if call("GET", detail.Cast[0].Image+login.Token, "", nil, nil) != 200 {
		t.Fatal("cast image unavailable")
	}
	if call("GET", detail.Cast[0].Image+viewer.Token, "", nil, nil) != 403 {
		t.Fatal("cast image leaked library permissions")
	}
	if call("GET", detail.Cast[0].Image, "", nil, nil) != 422 {
		t.Fatal("cast image missing authentication")
	}
	if call("GET", detail.Cast[0].Image+strings.Repeat("x", 64), "", nil, nil) != 401 {
		t.Fatal("invalid cast credential accepted")
	}
	if call("GET", "/api/v1/items/"+detail.Item.ID+"/cast/00000000-0000-4000-8000-000000000000/image?token="+login.Token, "", nil, nil) != 404 {
		t.Fatal("non-member cast image exposed")
	}
	castURL := "/api/v1/items?personId=" + detail.Cast[0].ID
	var castItems ItemsDTO
	if call("GET", castURL, login.Token, nil, &castItems) != 200 || len(castItems.Items) != 1 || castItems.Items[0].ID != detail.Item.ID {
		t.Fatalf("cast filmography missing: %+v", castItems)
	}
	if call("GET", castURL, viewer.Token, nil, &castItems) != 200 || len(castItems.Items) != 0 {
		t.Fatal("cast filmography leaked restricted library")
	}
	if call("GET", castURL, "", nil, nil) != 401 || call("GET", "/api/v1/items?personId=invalid", login.Token, nil, nil) != 422 {
		t.Fatal("cast filter auth or validation failed")
	}
	const otherCastItem = "e65918c7-e8a3-48f3-9b27-ac92a18ea5dd"
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,kind,title,sort_title,cast_members)
SELECT $1,library_id,'movie','ZZ Cast Film','zz cast film',cast_members FROM items WHERE id=$2`, otherCastItem, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if call("GET", castURL+"&limit=1", login.Token, nil, &castItems) != 200 || !castItems.HasMore || len(castItems.Items) != 1 {
		t.Fatalf("cast pagination failed: %+v", castItems)
	}
	if call("GET", "/api/v1/items?limit=1&cursor="+castItems.NextCursor, login.Token, nil, nil) != 422 {
		t.Fatal("cast cursor was accepted with a different filter")
	}
	if call("GET", castURL+"&limit=1&cursor="+castItems.NextCursor, login.Token, nil, &castItems) != 200 || castItems.HasMore || len(castItems.Items) != 1 || castItems.Items[0].ID != otherCastItem {
		t.Fatalf("cast next page failed: %+v", castItems)
	}
	const searchSeries = "e65918c7-e8a3-48f3-9b27-ac92a18ea5de"
	const searchEpisode = "e65918c7-e8a3-48f3-9b27-ac92a18ea5df"
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,kind,title,sort_title) VALUES
($1,$3,'','series','ZZ Cast Series','zz cast series'),
($2,$3,$1,'episode','ZZ Cast Episode','zz cast episode')`, searchSeries, searchEpisode, library.ID); err != nil {
		t.Fatal(err)
	}
	searchURL := "/api/v1/items?search=ZZ%20Cast&topLevel=true&limit=1"
	refreshURL := "/api/v1/items/" + searchSeries + "/metadata/refresh"
	for _, token := range []string{"", viewer.Token} {
		want := 403
		if token == "" {
			want = 401
		}
		if call("POST", refreshURL, token, MetadataRefreshRequest{Mode: "missing"}, nil) != want {
			t.Fatal("metadata refresh authorization bypassed")
		}
	}
	for _, body := range []any{map[string]string{}, MetadataRefreshRequest{Mode: "invalid"}, MetadataRefreshRequest{Mode: "missing"}} {
		if call("POST", refreshURL, login.Token, body, nil) != 422 {
			t.Fatal("invalid refresh request accepted")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET provider_id='42' WHERE id=$1`, searchSeries); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "replace"} {
		var refreshJob JobDTO
		if call("POST", refreshURL, login.Token, MetadataRefreshRequest{Mode: mode}, &refreshJob) != 200 {
			t.Fatal("refresh rejected")
		}
		var stored []byte
		err := pool.QueryRow(ctx, `SELECT payload FROM jobs WHERE id=$1`, refreshJob.ID).Scan(&stored)
		var payload MetadataRefreshRequest
		if err != nil || json.Unmarshal(stored, &payload) != nil || payload.Mode != mode {
			t.Fatalf("refresh mode not persisted: %s %v", stored, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, refreshJob.ID); err != nil {
			t.Fatal(err)
		}
	}
	var titles ItemsDTO
	if call("GET", searchURL, login.Token, nil, &titles) != 200 || len(titles.Items) != 1 || titles.Items[0].ID != otherCastItem || !titles.HasMore {
		t.Fatalf("top-level filter must run before pagination: %+v", titles)
	}
	if call("GET", "/api/v1/items?search=ZZ%20Cast&limit=1&cursor="+titles.NextCursor, login.Token, nil, nil) != 422 {
		t.Fatal("top-level cursor accepted without its filter")
	}
	if call("GET", searchURL+"&cursor="+titles.NextCursor, login.Token, nil, &titles) != 200 || len(titles.Items) != 1 || titles.Items[0].ID != searchSeries || titles.HasMore {
		t.Fatalf("top-level next page failed: %+v", titles)
	}
	if call("GET", searchURL, viewer.Token, nil, &titles) != 200 || len(titles.Items) != 0 {
		t.Fatal("top-level search leaked library access")
	}
	if call("GET", "/api/v1/items?parentId="+searchSeries, login.Token, nil, &titles) != 200 || len(titles.Items) != 1 || titles.Items[0].ID != searchEpisode {
		t.Fatal("episode listing regressed")
	}
	if call("GET", "/api/v1/items?topLevel=invalid", login.Token, nil, nil) != 422 {
		t.Fatal("invalid top-level filter accepted")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id IN ($1,$2,$3)`, otherCastItem, searchSeries, searchEpisode); err != nil {
		t.Fatal(err)
	}
	if call("PUT", "/api/v1/items/"+detail.Item.ID+"/metadata", login.Token, MetadataRequest{Title: detail.Item.Title, Year: detail.Item.Year}, nil) != 200 {
		t.Fatal("metadata edit failed")
	}
	call("GET", "/api/v1/items/"+detail.Item.ID, login.Token, nil, &detail)
	if len(detail.Cast) != 1 {
		t.Fatal("manual metadata edit discarded cast")
	}
	if f := detail.Files[0]; f.Size <= 0 || f.Width != 160 || f.Height != 90 {
		t.Fatalf("file technical details missing: %+v", f)
	}
	subtitleURL := "/api/v1/files/" + detail.Files[0].ID + "/subtitles"
	var uploaded SubtitleDTO
	if call("POST", subtitleURL, login.Token, SubtitleUpload{Name: "../personal.en.srt", Content: "1\n00:00:00,500 --> 00:00:03,000\nHello <b>world</b>\n"}, &uploaded) != 200 || uploaded.CueCount != 1 || uploaded.Name != "personal.en.srt" {
		t.Fatalf("upload: %+v", uploaded)
	}
	var subtitleList SubtitlesDTO
	if call("GET", subtitleURL, login.Token, nil, &subtitleList) != 200 || len(subtitleList.Items) != 1 {
		t.Fatal("saved subtitle missing")
	}
	var document SubtitleDocument
	if call("GET", subtitleURL+"/"+uploaded.ID, login.Token, nil, &document) != 200 || len(document.Cues) != 1 || document.Cues[0].Text != "Hello world" {
		t.Fatalf("subtitle document: %+v", document)
	}
	if call("POST", subtitleURL, login.Token, SubtitleUpload{Name: "bad.srt", Content: "not subtitles"}, nil) != 422 {
		t.Fatal("invalid subtitle accepted")
	}
	if call("GET", subtitleURL+"/"+uploaded.ID, viewer.Token, nil, nil) != 403 {
		t.Fatal("subtitle library permissions leaked")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO library_access(user_id,library_id) VALUES($1,$2)", user.ID, library.ID); err != nil {
		t.Fatal(err)
	}
	if call("GET", subtitleURL, viewer.Token, nil, &subtitleList) != 200 || len(subtitleList.Items) != 0 {
		t.Fatal("personal subtitles leaked to another viewer")
	}
	if call("GET", subtitleURL+"/"+uploaded.ID, viewer.Token, nil, nil) != 404 {
		t.Fatal("personal subtitle document leaked")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM library_access WHERE user_id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	var metadataJob JobDTO
	if call("POST", "/api/v1/items/"+detail.Item.ID+"/identify", login.Token, IdentifyRequest{ProviderID: 123}, &metadataJob) != 200 {
		t.Fatal("metadata task enqueue failed")
	}
	var jobs JobsDTO
	if call("GET", "/api/v1/admin/jobs", login.Token, nil, &jobs) != 200 {
		t.Fatal("job details failed")
	}
	found := false
	for _, j := range jobs.Items {
		if j.ID == metadataJob.ID {
			found = true
			if j.ResourceName != detail.Item.Title || j.ItemID != detail.Item.ID || j.LibraryID != library.ID || j.Role != "scanner" || j.UpdatedAt.IsZero() {
				t.Fatalf("metadata task context missing: %+v", j)
			}
		}
	}
	if !found {
		t.Fatal("metadata job missing from history")
	}
	if call("GET", "/api/v1/admin/jobs", viewer.Token, nil, nil) != 403 {
		t.Fatal("job details exposed to viewer")
	}
	var playback PlaybackDTO
	request := PlaybackRequest{FileID: detail.Files[0].ID, AudioIndex: -1, SubtitleIndex: -1, DirectPlay: true}
	if call("POST", "/api/v1/playback", login.Token, request, &playback) != 200 || playback.Method != "direct" {
		t.Fatalf("direct: %+v", playback)
	}
	req := httptest.NewRequest("GET", playback.URL+"?token="+playback.StreamToken, nil)
	req.Header.Set("Range", "bytes=0-31")
	res, e := s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || len(b) != 32 {
		t.Fatalf("range %d %d", res.StatusCode, len(b))
	}
	call("POST", "/api/v1/playback/"+playback.ID+"/progress", login.Token, ProgressRequest{Sequence: 1, Position: playback.Duration * 0.25}, nil)
	var resume ItemsDTO
	if call("GET", "/api/v1/items?resume=true&limit=8", login.Token, nil, &resume) != 200 || len(resume.Items) != 1 || resume.Items[0].Position != playback.Duration*0.25 || resume.Items[0].Duration != playback.Duration {
		t.Fatalf("resume catalog: %+v", resume)
	}
	if call("PATCH", "/api/v1/admin/settings", login.Token, map[string]any{"watchedPercent": 50}, nil) != 200 {
		t.Fatal("watched threshold update failed")
	}
	call("POST", "/api/v1/playback/"+playback.ID+"/progress", login.Token, ProgressRequest{Sequence: 2, Position: playback.Duration * 0.6}, nil)
	call("POST", "/api/v1/playback/"+playback.ID+"/progress", login.Token, ProgressRequest{Sequence: 1, Position: 1}, nil)
	call("GET", "/api/v1/items/"+items.Items[0].ID, login.Token, nil, &detail)
	if detail.Item.Position != playback.Duration*0.6 || !detail.Item.Watched {
		t.Fatal("stale progress overwrote newer sample")
	}
	if call("GET", "/api/v1/items?resume=true", login.Token, nil, &resume) != 200 || len(resume.Items) != 0 {
		t.Fatalf("completed item remains in resume: %+v", resume)
	}
	call("DELETE", "/api/v1/playback/"+playback.ID, login.Token, nil, nil)
	if call("GET", playback.URL+"?token="+playback.StreamToken, "", nil, nil) != 403 {
		t.Fatal("stopped token usable")
	}
	request.MaxHeight = 480
	if call("POST", "/api/v1/playback", login.Token, request, nil) != 422 {
		t.Fatal("unsupported resolution accepted")
	}
	request.DirectPlay = false
	request.MaxBitrate = 0
	request.MaxHeight = 720
	// Adding a sidecar must be detected without touching the already scanned video.
	sidecar := strings.TrimSuffix(mediaPath, ".mp4") + ".en.srt"
	if e = os.WriteFile(sidecar, []byte("1\n00:00:00,000 --> 00:00:03,000\nHello JFE\n"), 0600); e != nil {
		t.Fatal(e)
	}
	call("POST", "/api/v1/libraries/"+library.ID+"/scan", login.Token, nil, &job)
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		call("GET", "/api/v1/items/"+items.Items[0].ID, login.Token, nil, &detail)
		for _, track := range detail.Files[0].Tracks {
			if track.Type == "subtitle" {
				request.SubtitleIndex = track.Index
			}
		}
		if request.SubtitleIndex >= 10000 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if request.SubtitleIndex < 10000 {
		t.Fatal("rescan did not discover new sidecar")
	}
	var encoding EncodingDTO
	if call("GET", "/api/v1/admin/encoding", login.Token, nil, &encoding) != 200 || encoding.Mode != "disabled" {
		t.Fatalf("default encoding: %+v", encoding)
	}
	var system SystemDTO
	call("GET", "/api/v1/system", "", nil, &system)
	if system.Capabilities.Transcoding {
		t.Fatal("transcoding enabled by default")
	}
	encoding.Mode = "cpu"
	if call("PUT", "/api/v1/admin/encoding", login.Token, encoding, nil) != 422 {
		t.Fatal("CPU mode accepted")
	}
	var before, after int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM playback_sessions").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if call("POST", "/api/v1/playback", login.Token, request, nil) != 409 {
		t.Fatal("disabled subtitle/quality encoding accepted")
	}
	request.SubtitleIndex = -1
	request.ForceTranscode = true
	if call("POST", "/api/v1/playback", login.Token, request, nil) != 409 {
		t.Fatal("disabled forced encoding accepted")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM playback_sessions").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("rejected encoding created a session")
	}
	request.ForceTranscode = false
	// The 720p ceiling is above this fixture; keep its original video.
	if call("POST", "/api/v1/playback", login.Token, request, &playback) != 200 || playback.Method != "remux" {
		t.Fatalf("disabled remux: %+v", playback)
	}
	streamToken := playback.StreamToken
	deadline = time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		call("GET", "/api/v1/playback/"+playback.ID, login.Token, nil, &playback)
		if playback.State == "ready" || playback.State == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if playback.State != "ready" {
		t.Fatalf("HLS: %+v", playback)
	}
	// Measurements arrive independently after the session becomes playable.
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until) && (playback.Stream == nil || playback.Stream.BitrateSource == "pending"); {
		time.Sleep(50 * time.Millisecond)
		call("GET", "/api/v1/playback/"+playback.ID, login.Token, nil, &playback)
	}
	if playback.Stream == nil || playback.Stream.VideoTranscoded || playback.Stream.VideoBitrate <= 0 || playback.Stream.AudioBitrate <= 0 || playback.Stream.BitrateSource != "segment" {
		t.Fatalf("remux stream metadata: %+v", playback.Stream)
	}
	req = httptest.NewRequest("GET", playback.URL+"?token="+streamToken, nil)
	res, e = s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(b, []byte("#EXT-X-PLAYLIST-TYPE:EVENT")) || !bytes.Contains(b, []byte("?token=")) {
		t.Fatalf("manifest: %d %s", res.StatusCode, b)
	}
	call("DELETE", "/api/v1/playback/"+playback.ID, login.Token, nil, nil)

	// Source-aware clients keep originals native and use fMP4 for remux.
	var sourceVideo TrackDTO
	for _, track := range detail.Files[0].Tracks {
		if track.Type == "video" {
			sourceVideo = track
		}
	}
	capabilities := &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Audio: []string{"aac"}, Remux: true, Video: []VideoCapabilityDTO{{Codec: sourceVideo.Codec, Profile: sourceVideo.Profile, BitDepth: sourceVideo.BitDepth, Level: sourceVideo.Level, MaxWidth: sourceVideo.Width, MaxHeight: sourceVideo.Height}}}
	modern := PlaybackRequest{FileID: request.FileID, AudioIndex: -1, SubtitleIndex: -1, DirectPlay: true, Capabilities: capabilities}
	if call("POST", "/api/v1/playback", login.Token, modern, &playback) != 200 || playback.Decision == nil || playback.Decision.Mode != PlaybackModeDirectPlay || playback.Protocol != "file" {
		t.Fatalf("modern original: %+v", playback)
	}
	var directJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE resource_id=$1", playback.ID).Scan(&directJobs); err != nil || directJobs != 0 {
		t.Fatalf("direct play enqueued FFmpeg: %d %v", directJobs, err)
	}
	call("DELETE", "/api/v1/playback/"+playback.ID, login.Token, nil, nil)
	// A direct decode/container failure restarts at the requested position with copy.
	modern.DirectPlay = false
	modern.Position = 1
	if call("POST", "/api/v1/playback", login.Token, modern, &playback) != 200 || playback.Protocol != "mp4" || playback.Decision.VideoAction != "copy" || playback.Decision.AudioAction != "copy" {
		t.Fatalf("modern remux: %+v", playback)
	}
	streamToken = playback.StreamToken
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		call("GET", "/api/v1/playback/"+playback.ID, login.Token, nil, &playback)
		if playback.State == "ready" || playback.State == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if playback.State != "ready" || playback.Position != 1 {
		t.Fatalf("fMP4: %+v", playback)
	}
	req = httptest.NewRequest("GET", playback.URL+"?token="+streamToken, nil)
	req.Header.Set("Range", "bytes=0-1023")
	res, e = s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || !strings.HasPrefix(res.Header.Get("Content-Type"), "video/mp4") || !bytes.Contains(b, []byte("ftyp")) {
		t.Fatalf("fMP4 response: %d %v", res.StatusCode, res.Header)
	}
	req = httptest.NewRequest("GET", playback.URL+"?token=invalid", nil)
	res, e = s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("remux token not enforced")
	}
	call("DELETE", "/api/v1/playback/"+playback.ID, login.Token, nil, nil)
	req = httptest.NewRequest("GET", playback.URL+"?token="+streamToken, nil)
	res, e = s.App.Test(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("stopped remux still accessible")
	}
	// Scanner-prepared sidecar cues are available through the existing overlay API.
	var textSubtitles SubtitlesDTO
	call("GET", subtitleURL, login.Token, nil, &textSubtitles)
	prepared := false
	for _, sub := range textSubtitles.Items {
		if !strings.HasSuffix(sub.Name, " srt") {
			continue
		}
		var document SubtitleDocument
		if call("GET", subtitleURL+"/"+sub.ID, login.Token, nil, &document) != 200 || len(document.Cues) != 1 || document.Cues[0].Text != "Hello JFE" {
			t.Fatalf("prepared subtitles: %+v", document)
		}
		prepared = true
	}
	if !prepared {
		t.Fatal("scanner did not prepare text subtitles")
	}
	encoding.Mode = "nvidia"
	if call("PUT", "/api/v1/admin/encoding", login.Token, encoding, nil) != 200 {
		t.Fatal("NVIDIA mode rejected")
	}
	call("GET", "/api/v1/admin/encoding", login.Token, nil, &encoding)
	call("GET", "/api/v1/system", "", nil, &system)
	if encoding.Mode != "nvidia" || !system.Capabilities.Transcoding {
		t.Fatal("NVIDIA setting/capability not persisted")
	}
	if os.Getenv("JFE_TEST_NVENC") != "1" {
		t.Log("NVENC hardware execution not tested; set JFE_TEST_NVENC=1 on an NVIDIA host")
		return
	}
	request.MaxHeight = 720
	request.ForceTranscode = true
	if call("POST", "/api/v1/playback", login.Token, request, &playback) != 200 || playback.Method != "transcode" {
		t.Fatalf("NVENC negotiation: %+v", playback)
	}
	deadline = time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		call("GET", "/api/v1/playback/"+playback.ID, login.Token, nil, &playback)
		if playback.State == "ready" || playback.State == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if playback.State != "ready" {
		t.Fatalf("NVENC HLS: %+v", playback)
	}
	if _, err := os.Stat(filepath.Join(cfg.CacheRoot, "playback", playback.ID, "index.m3u8")); err != nil {
		t.Fatal(err)
	}

}

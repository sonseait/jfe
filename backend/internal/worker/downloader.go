package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type DownloadSettings struct {
	MaxConcurrent int `json:"maxConcurrent"`
	MinFreeGiB    int `json:"minFreeGiB"`
}

func freeDisk(root string, minGiB int) error {
	var st syscall.Statfs_t
	if e := syscall.Statfs(root, &st); e != nil {
		return e
	}
	if uint64(st.Bavail)*uint64(st.Bsize) < uint64(minGiB)*(1<<30) {
		return fmt.Errorf("%w: minimum %d GiB", audio.ErrStorageFull, minGiB)
	}
	return nil
}
func (w *Worker) sourceAllowed(ctx context.Context, s store.ImportSource) error {
	current, e := w.DB.GetImportSource(ctx, s.ID)
	if e != nil {
		return e
	}
	if current.Paused {
		return errors.New("source paused")
	}
	return w.importPermission(ctx, s.UserID, s.LibraryID)
}
func (w *Worker) youtubeJob(ctx context.Context, j store.Job) error {
	source, e := w.DB.GetImportSource(ctx, j.ResourceID)
	if e != nil {
		return e
	}
	if e = w.sourceAllowed(ctx, source); e != nil {
		return e
	}
	if w.Config.ImportRoot == "" {
		return errors.New("import root is not configured")
	}
	if j.Kind == "youtube_preview" {
		listing, e := audio.PreviewYouTube(ctx, source.Url)
		if e != nil {
			return e
		}
		old, e := w.DB.ListImportEntries(ctx, source.ID)
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		ordinal := 0
		for _, entry := range old {
			seen[entry.VideoID] = true
			ordinal = max(ordinal, int(entry.Ordinal))
		}
		for _, entry := range listing.Entries {
			if _, e = audio.YouTubeURL("https://www.youtube.com/watch?v=" + entry.ID); e != nil {
				continue
			}
			if seen[entry.ID] {
				continue
			}
			ordinal++
			if e = w.validLease(ctx, j); e != nil {
				return e
			}
			if e = w.DB.SaveImportEntry(ctx, store.SaveImportEntryParams{SourceID: source.ID, VideoID: entry.ID, Title: entry.Title, Ordinal: int32(ordinal)}); e != nil {
				return e
			}
		}
		if e = w.DB.ScheduleImportSource(ctx, store.ScheduleImportSourceParams{ID: source.ID, Column2: listing.Title}); e != nil {
			return e
		}
		if source.Follow {
			_, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: "youtube_download", ResourceID: source.ID, Payload: []byte("{}")})
		}
		return e
	}
	settings := DownloadSettings{1, 5}
	if data, err := w.DB.GetSetting(ctx, "downloads"); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	entries, e := w.DB.ListImportEntries(ctx, source.ID)
	if e != nil {
		return e
	}
	failures := 0
	for n, entry := range entries {
		if e = w.validLease(ctx, j); e != nil {
			return e
		}
		if e = w.sourceAllowed(ctx, source); e != nil {
			return e
		}
		if entry.State == "ready" {
			continue
		}
		e = w.downloadEntry(ctx, j, source, entry, settings)
		state, message := "ready", ""
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures++
			state = "failed"
			message = audio.JobErrorCode(e)
			if message == "" {
				message = audio.ErrYouTubeRequest.Error()
			}
			log.Error().Err(e).Str("jobId", j.ID).Str("sourceId", source.ID).
				Str("videoId", entry.VideoID).Str("errorCode", message).Msg("YouTube entry download failed")
		}
		if e = w.DB.SetImportEntry(ctx, store.SetImportEntryParams{SourceID: source.ID, VideoID: entry.VideoID, State: state, Error: message}); e != nil {
			return e
		}
		_, e = w.DB.ReportScan(ctx, store.ReportScanParams{ID: j.ID, LeaseID: j.LeaseID, TotalFiles: int32(len(entries)), ProcessedFiles: int32(n + 1), Progress: int32((n + 1) * 99 / max(1, len(entries)))})
		if e != nil {
			return e
		}
	}
	_, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "scan", ResourceID: source.LibraryID, Payload: []byte("{}")})
	if e != nil {
		return e
	}
	if failures > 0 {
		return audio.ErrYouTubePartial
	}
	return nil
}
func (w *Worker) downloadEntry(ctx context.Context, j store.Job, s store.ImportSource, entry store.ImportEntry, settings DownloadSettings) error {
	unlock, e := audio.Lock(ctx, w.Config.CacheRoot, "youtube:"+s.LibraryID+":"+entry.VideoID)
	if e != nil {
		return e
	}
	defer unlock()
	if _, e = w.DB.GetImportedFile(ctx, store.GetImportedFileParams{LibraryID: s.LibraryID, VideoID: entry.VideoID}); e == nil {
		return nil
	}
	root, e := filepath.EvalSymlinks(w.Config.ImportRoot)
	if e != nil {
		return e
	}
	dest := filepath.Join(root, s.LibraryID, s.ID, entry.VideoID)
	// A ready directory is the durable commit record if the DB write was interrupted.
	if _, e = os.Stat(filepath.Join(dest, ".ready")); e == nil {
		return w.recordImport(ctx, s, entry, dest)
	}
	stage := filepath.Join(root, ".staging", j.ID+"-"+j.LeaseID+"-"+entry.VideoID)
	if e = os.MkdirAll(stage, 0750); e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if _, e = media.Within(root, stage); e != nil {
		return e
	}
	if e = freeDisk(root, settings.MinFreeGiB); e != nil {
		return e
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 4*time.Hour)
	defer cancel()
	runCtx, stop := context.WithCancelCause(timeoutCtx)
	done := make(chan struct{})
	defer func() { stop(nil); cancel(); <-done }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := freeDisk(root, settings.MinFreeGiB); err != nil {
					stop(err)
					return
				}
				if err := w.sourceAllowed(runCtx, s); err != nil {
					stop(err)
					return
				}
			}
		}
	}()
	_, e = audio.YouTubeCommand(runCtx, "--no-playlist", "--format", "bestaudio[ext=m4a]/bestaudio[ext=webm]", "--write-info-json", "--write-thumbnail", "--convert-thumbnails", "jpg", "--output", filepath.Join(stage, "audio.%(ext)s"), "--", "https://www.youtube.com/watch?v="+entry.VideoID)
	if e != nil {
		if runCtx.Err() != nil {
			e = context.Cause(runCtx)
		}
		return fmt.Errorf("download audio with yt-dlp: %w", e)
	}
	files, e := os.ReadDir(stage)
	if e != nil {
		return e
	}
	var path string
	for _, f := range files {
		if f.Name() == "audio.m4a" || f.Name() == "audio.webm" {
			path = filepath.Join(stage, f.Name())
		}
	}
	if path == "" {
		return errors.New("no audio file downloaded")
	}
	if strings.HasSuffix(path, ".webm") {
		out := filepath.Join(stage, "audio.opus")
		if e = remuxDownloaded(runCtx, path, out); e != nil {
			return e
		}
		_ = os.Remove(path)
		path = out
	}
	probe, e := media.Inspect(runCtx, path)
	if e != nil {
		return fmt.Errorf("probe downloaded audio: %w", e)
	}
	if probe.Duration() <= 0 {
		return errors.New("invalid downloaded audio")
	}
	tags := audio.Tags{Title: entry.Title, Album: s.Title, Track: strconv.Itoa(int(entry.Ordinal)), Artists: []string{}, AlbumArtists: []string{}, Genres: []string{}}
	var info struct {
		Artist, Uploader, Album string
		Chapters                []struct {
			Start float64 `json:"start_time"`
			End   float64 `json:"end_time"`
			Title string  `json:"title"`
		} `json:"chapters"`
	}
	if b, err := os.ReadFile(filepath.Join(stage, "audio.info.json")); err == nil {
		_ = json.Unmarshal(b, &info)
	}
	if info.Artist != "" {
		tags.Artists = []string{info.Artist}
	} else if info.Uploader != "" {
		tags.Artists = []string{info.Uploader}
	}
	if info.Album != "" {
		tags.Album = info.Album
	}
	if tags.Album == "" {
		tags.Album = s.ID
	}
	patch := audio.Patch{Title: &tags.Title, Album: &tags.Album, Artists: &tags.Artists, Track: &tags.Track}
	if picture, err := os.ReadFile(filepath.Join(stage, "audio.jpg")); err == nil && audio.ValidArtwork(picture) {
		encoded := base64.StdEncoding.EncodeToString(picture)
		patch.Artwork = &encoded
	}
	chapters := []media.Chapter{}
	for _, c := range info.Chapters {
		chapter := media.Chapter{Start: media.Scalar(strconv.FormatFloat(c.Start, 'f', 6, 64)), End: media.Scalar(strconv.FormatFloat(c.End, 'f', 6, 64)), Tags: map[string]string{"title": c.Title}}
		chapters = append(chapters, chapter)
	}
	if len(chapters) > 0 {
		data, _ := json.Marshal(chapters)
		if e = os.WriteFile(path+".chapters.json", data, 0640); e != nil {
			return e
		}
	}

	if _, e = audio.Helper(runCtx, w.Config.Python, "write", path, &patch); e != nil {
		return fmt.Errorf("write downloaded audio tags: %w", e)
	}
	b, _ := json.Marshal(tags)
	if e = os.WriteFile(path+".json", b, 0640); e != nil {
		return e
	}
	_ = os.Remove(filepath.Join(stage, "audio.info.json"))
	_ = os.Remove(filepath.Join(stage, "audio.jpg"))
	if e = os.WriteFile(filepath.Join(stage, ".ready"), []byte(filepath.Base(path)), 0600); e != nil {
		return e
	}
	if e = w.validLease(ctx, j); e != nil {
		return e
	}
	if e = w.sourceAllowed(ctx, s); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(dest), 0750); e != nil {
		return e
	}
	if _, e = media.Within(root, filepath.Dir(dest)); e != nil {
		return e
	}
	if e = os.Rename(stage, dest); e != nil {
		return e
	}
	return w.recordImport(ctx, s, entry, dest)
}
func (w *Worker) recordImport(ctx context.Context, s store.ImportSource, entry store.ImportEntry, dir string) error {
	b, e := os.ReadFile(filepath.Join(dir, ".ready"))
	if e != nil {
		return e
	}
	name := string(b)
	if name != "audio.m4a" && name != "audio.opus" {
		return fmt.Errorf("invalid import marker")
	}
	path, e := media.Within(w.Config.ImportRoot, filepath.Join(dir, name))
	if e != nil {
		return e
	}
	return w.DB.SaveImportedFile(ctx, store.SaveImportedFileParams{LibraryID: s.LibraryID, VideoID: entry.VideoID, Path: path})
}
func (w *Worker) scheduleDownloads(ctx context.Context) {
	sources, e := w.DB.DueImportSources(ctx)
	if e != nil {
		return
	}
	for _, s := range sources {
		if w.sourceAllowed(ctx, s) != nil {
			continue
		}
		_, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: "youtube_preview", ResourceID: s.ID, Payload: []byte("{}")})
		if e == nil {
			_ = w.DB.ScheduleImportSource(ctx, store.ScheduleImportSourceParams{ID: s.ID})
		}
	}
}

func (w *Worker) claimDownload(ctx context.Context) (store.Job, error) {
	tx, e := w.Pool.Begin(ctx)
	if e != nil {
		return store.Job{}, e
	}
	defer tx.Rollback(ctx)
	q := w.DB.WithTx(tx)
	if e = q.LockDownloads(ctx); e != nil {
		return store.Job{}, e
	}
	b, e := q.GetSetting(ctx, "downloads")
	if e != nil {
		return store.Job{}, e
	}
	cfg := DownloadSettings{1, 5}
	if e = json.Unmarshal(b, &cfg); e != nil {
		return store.Job{}, e
	}
	active, e := q.ActiveDownloads(ctx)
	if e != nil {
		return store.Job{}, e
	}
	if active >= int64(cfg.MaxConcurrent) {
		return store.Job{}, pgx.ErrNoRows
	}
	j, e := q.ClaimJob(ctx, store.ClaimJobParams{Role: "downloader", LeaseID: uuid.NewString()})
	if e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}

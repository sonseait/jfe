package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/config"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Worker struct {
	Config   config.Config
	DB       *store.Queries
	Pool     *pgxpool.Pool
	Role, ID string
}

func Run(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, role string) error {
	w := &Worker{cfg, store.New(pool), pool, role, uuid.NewString()}
	var wg sync.WaitGroup
	python := cfg.Python
	if python == "" {
		python = "python3"
	}
	audioReady := exec.CommandContext(ctx, python, "-c", "import mutagen").Run() == nil
	if _, e := exec.LookPath("ffprobe"); e != nil {
		audioReady = false
	}
	youtubeReady := audioReady && cfg.ImportRoot != ""
	if stat, e := os.Stat(cfg.ImportRoot); e != nil || !stat.IsDir() {
		youtubeReady = false
	}
	if _, e := exec.LookPath("yt-dlp"); e != nil {
		youtubeReady = false
	}
	if _, e := exec.LookPath("node"); e != nil {
		youtubeReady = false
	}
	_, subtitleFF := exec.LookPath("ffmpeg")
	_, subtitleProbe := exec.LookPath("ffprobe")
	_, subtitleMKV := exec.LookPath("mkvmerge")
	_, subtitleExtract := exec.LookPath("mkvextract")
	subtitleReady := cfg.SubtitleEditing && subtitleFF == nil && subtitleProbe == nil
	caps, _ := json.Marshal(struct {
		SubtitleSync bool `json:"subtitleSync"`
		SubtitleMKV  bool `json:"subtitleMKV"`
		Audio        bool `json:"audio"`
		YouTube      bool `json:"youtube"`
	}{subtitleReady, subtitleReady && subtitleMKV == nil && subtitleExtract == nil, audioReady, youtubeReady})

	slots := cfg.Concurrency
	if role == "transcoder" || role == "downloader" {
		slots = 16
	}
	for n := 0; n < slots; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.loop(ctx) }()
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if e := w.DB.Heartbeat(ctx, store.HeartbeatParams{ID: w.ID, Role: role}); e != nil {
			log.Error().Err(e).Msg("worker heartbeat failed")
		}
		_ = w.DB.SetWorkerCapabilities(ctx, store.SetWorkerCapabilitiesParams{ID: w.ID, Capabilities: caps})
		_ = w.DB.ReapJobs(ctx)
		_ = w.DB.ExpirePlayback(ctx)
		_ = w.DB.ReapPlayback(ctx)
		if role == "transcoder" {
			w.cleanup(ctx)
		}
		if role == "scanner" {
			w.schedule(ctx)
			w.cleanupAudio(ctx)
			w.recoverSubtitles(ctx)
		}
		if role == "downloader" {
			w.scheduleDownloads(ctx)
			w.cleanupAudio(ctx)
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
		}
	}
}
func (w *Worker) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		j, e := w.claim(ctx)
		if e != nil {
			if !errors.Is(e, pgx.ErrNoRows) {
				log.Error().Err(e).Msg("job claim failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		w.runJob(ctx, j)
	}
}
func (w *Worker) runJob(ctx context.Context, j store.Job) {
	jobCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if _, e := w.DB.RenewJob(jobCtx, store.RenewJobParams{ID: j.ID, LeaseID: j.LeaseID}); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	var err error
	switch j.Kind {
	case "youtube_preview", "youtube_download":
		err = w.youtubeJob(jobCtx, j)
	case "subtitle_sync":
		err = w.subtitleJob(jobCtx, j)
	case "audio_tags":
		err = w.writeAudioTags(jobCtx, j)
	case "scan":
		var options ScanOptions
		if err = json.Unmarshal(j.Payload, &options); err != nil {
			break
		}
		err = w.scan(jobCtx, j.ResourceID, options, func(done, total int) error {
			progress := 0
			if total > 0 {
				progress = min(99, done*100/total)
			}
			_, err := w.DB.ReportScan(jobCtx, store.ReportScanParams{ID: j.ID, LeaseID: j.LeaseID, TotalFiles: int32(total), ProcessedFiles: int32(done), Progress: int32(progress)})
			return err
		})
	case "metadata":
		err = w.metadata(jobCtx, j)
	case "playback":
		err = w.transcode(jobCtx, j)
	default:
		err = errors.New("unsupported job kind")
	}
	state, message := "completed", ""
	if jobCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, errPlaybackStopped) {
		state = "cancelled"
		log.Info().Str("jobId", j.ID).Msg("job cancelled")
	} else if err != nil {
		state = "failed"
		if (w.Role == "scanner" || w.Role == "downloader") && j.Attempts < 3 && !errors.Is(err, media.ErrNeedsIdentification) && !errors.Is(err, media.ErrTMDBNotConfigured) {
			state = "pending"
		}
		message = "Job failed; inspect worker logs"
		if j.Kind == "subtitle_sync" {
			record, e := w.DB.GetSubtitleSyncJob(jobCtx, j.ID)
			if e == nil && record.ErrorCode != "" {
				message = record.ErrorCode
			}
		}
		if code := audio.JobErrorCode(err); code != "" && (j.Kind == "youtube_preview" || j.Kind == "youtube_download" || j.Kind == "audio_tags") {
			message = code
		}
		if errors.Is(err, audio.ErrYouTubeLogin) || errors.Is(err, audio.ErrYouTubeUnavailable) {
			state = "failed"
		}
		if errors.Is(err, media.ErrNeedsIdentification) {
			message = media.ErrNeedsIdentification.Error()
		}
		if errors.Is(err, media.ErrTMDBNotConfigured) {
			message = media.ErrTMDBNotConfigured.Error()
		}
		var providerErr *media.ProviderError
		if errors.As(err, &providerErr) {
			message = providerErr.Error()
		}
		log.Error().Err(err).Str("jobId", j.ID).Str("kind", j.Kind).
			Str("resourceId", j.ResourceID).Int32("attempt", j.Attempts).Msg("job failed")
	}
	cancel()
	<-done
	finishCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	if err := w.DB.FinishJob(finishCtx, store.FinishJobParams{ID: j.ID, LeaseID: j.LeaseID, State: state, Error: message}); err != nil {
		log.Error().Err(err).Msg("job completion failed")
	}
}
func (w *Worker) schedule(ctx context.Context) {
	ls, e := w.DB.DueLibraries(ctx)
	if e != nil {
		return
	}
	for _, l := range ls {
		_, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "scan", ResourceID: l.ID, Payload: []byte("{}")})
		if e != nil {
			log.Error().Err(e).Msg("scheduled scan enqueue failed")
		}
	}
}

func (w *Worker) claim(ctx context.Context) (store.Job, error) {
	if w.Role == "downloader" {
		return w.claimDownload(ctx)
	}
	if w.Role != "transcoder" {
		return w.DB.ClaimJob(ctx, store.ClaimJobParams{Role: w.Role, LeaseID: uuid.NewString()})
	}
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return store.Job{}, err
	}
	defer tx.Rollback(ctx)
	q := w.DB.WithTx(tx)
	if err = q.LockTranscodes(ctx); err != nil {
		return store.Job{}, err
	}
	data, err := q.GetSetting(ctx, "encoding")
	if err != nil {
		return store.Job{}, err
	}
	var encoding struct{ MaxConcurrent int }
	if err = json.Unmarshal(data, &encoding); err != nil {
		return store.Job{}, err
	}
	active, err := q.ActiveTranscodes(ctx)
	if err != nil {
		return store.Job{}, err
	}
	if active >= int64(encoding.MaxConcurrent) {
		return store.Job{}, pgx.ErrNoRows
	}
	job, err := q.ClaimJob(ctx, store.ClaimJobParams{Role: w.Role, LeaseID: uuid.NewString()})
	if err != nil {
		return job, err
	}
	return job, tx.Commit(ctx)
}

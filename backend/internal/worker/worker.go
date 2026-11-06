package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/config"
	"jfe/backend/internal/media"
	"jfe/backend/internal/observability"
	"jfe/backend/internal/store"
	"jfe/backend/internal/subtitleprovider"
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
	logger := log.Ctx(ctx).With().Str("workerRole", role).Str("workerId", w.ID).Logger()
	ctx = logger.WithContext(ctx)
	defer log.Ctx(ctx).Info().Msg("worker stopped")
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
		OpenSubtitles bool `json:"openSubtitles"`
		AutoSubtitles bool `json:"autoSubtitles"`
		SubtitleSync  bool `json:"subtitleSync"`
		SubtitleMKV   bool `json:"subtitleMKV"`
		Audio         bool `json:"audio"`
		YouTube       bool `json:"youtube"`
	}{cfg.OpenSubtitlesKey != "", cfg.SubtitleDownloads && cfg.OpenSubtitlesKey != "" && subtitleFF == nil && subtitleProbe == nil, subtitleReady, subtitleReady && subtitleMKV == nil && subtitleExtract == nil, audioReady, youtubeReady})

	slots := cfg.Concurrency
	if role == "transcoder" || role == "downloader" {
		slots = 16
	}
	log.Ctx(ctx).Info().Int("concurrency", slots).RawJSON("capabilities", caps).Msg("worker started")
	for n := 0; n < slots; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.loop(ctx) }()
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if e := w.DB.Heartbeat(ctx, store.HeartbeatParams{ID: w.ID, Role: role}); e != nil {
			log.Ctx(ctx).Error().Err(e).Msg("worker heartbeat failed")
		}
		w.maintenance(ctx, "publish_capabilities", func(ctx context.Context) error {
			return w.DB.SetWorkerCapabilities(ctx, store.SetWorkerCapabilitiesParams{ID: w.ID, Capabilities: caps})
		})
		w.maintenance(ctx, "reap_jobs", w.DB.ReapJobs)
		w.maintenance(ctx, "expire_playback", w.DB.ExpirePlayback)
		w.maintenance(ctx, "reap_playback", w.DB.ReapPlayback)
		if role == "transcoder" {
			w.cleanup(ctx)
		}
		if role == "scanner" {
			w.schedule(ctx)
			w.cleanupAudio(ctx)
			w.recoverSubtitles(ctx)
			if e := w.scheduleAutoSubtitles(ctx); e != nil {
				log.Ctx(ctx).Error().Err(e).Msg("automatic subtitle scheduling failed")
			}
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
				log.Ctx(ctx).Error().Err(e).Msg("job claim failed")
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
	startedAt := time.Now()
	logger := w.jobLogger(ctx, j)
	startCtx, startCancel := context.WithTimeout(ctx, 3*time.Second)
	startErr := w.DB.StartJobRun(startCtx, store.StartJobRunParams{ID: j.LeaseID, JobID: j.ID, Attempt: j.Attempts, WorkerID: w.ID})
	startCancel()
	if startErr != nil {
		logger.Error().Err(startErr).Msg("could not create job execution history")
		finishCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		state := "pending"
		if j.Attempts >= 3 {
			state = "failed"
		}
		if _, e := w.DB.FinishJob(finishCtx, store.FinishJobParams{ID: j.ID, LeaseID: j.LeaseID, State: state, Error: "job_history_unavailable"}); e != nil {
			logger.Error().Err(e).Msg("could not release job after history failure")
		}
		return
	}
	history := newJobLogWriter(w.DB, j.LeaseID, w.Config)
	logger = logger.Output(history).Level(zerolog.DebugLevel)
	runState := "interrupted"
	defer func() {
		dropped := history.close()
		finishCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		if e := w.DB.FinishJobRun(finishCtx, store.FinishJobRunParams{ID: j.LeaseID, State: runState, DroppedLogs: dropped}); e != nil {
			log.Ctx(ctx).Error().Err(e).Str("jobId", j.ID).Str("runId", j.LeaseID).Msg("could not finish job execution history")
		}
	}()
	jobCtx, cancel := context.WithCancel(logger.WithContext(ctx))
	jobCtx = context.WithValue(jobCtx, jobTraceKey{}, &jobTrace{stage: "initialize"})
	log.Ctx(jobCtx).Info().Dur("queueWaitMs", max(0, startedAt.Sub(j.NextAttemptAt))).Msg("job started")
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
					observability.Error(log.Ctx(jobCtx).Warn(), e).Str("stage", "renew_lease").Msg("job lease renewal failed; cancelling execution")
					cancel()
					return
				}
			}
		}
	}()
	var err error
	jobStage(jobCtx, j.Kind)
	switch j.Kind {
	case "youtube_preview", "youtube_download":
		err = w.youtubeJob(jobCtx, j)
	case "subtitle_auto":
		err = w.automaticSubtitle(jobCtx, j)
	case "subtitle_provider_check":
		err = subtitleprovider.Do(jobCtx, w.Pool, w.Config, func(ctx context.Context, client *subtitleprovider.Client) error { return client.Account(ctx) })
	case "subtitle_download":
		err = w.downloadSubtitle(jobCtx, j)
	case "subtitle_prepare":
		err = w.prepareSubtitleJob(jobCtx, j)
	case "subtitle_sync":
		err = w.subtitleJob(jobCtx, j)
	case "audio_tags":
		err = w.writeAudioTags(jobCtx, j)
	case "scan":
		var options ScanOptions
		if err = json.Unmarshal(j.Payload, &options); err != nil {
			break
		}
		var lastReport time.Time
		var lastProgressLog time.Time
		jobStage(jobCtx, "scan")
		err = w.scan(jobCtx, j.ResourceID, options, func(done, total int) error {
			jobProgress(jobCtx, done, total)
			if total > 0 && (done == 0 || done == total || time.Since(lastProgressLog) >= 15*time.Second) {
				jobFields(jobCtx, log.Ctx(jobCtx).Info()).Msg("scan progress")
				lastProgressLog = time.Now()
			}
			if done != 0 && done != total && time.Since(lastReport) < 250*time.Millisecond {
				return jobCtx.Err()
			}
			lastReport = time.Now()
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
		reason := "context_cancelled"
		if ctx.Err() != nil {
			reason = "worker_shutdown"
		} else if errors.Is(err, errPlaybackStopped) {
			reason = "playback_stopped"
		}
		jobFields(jobCtx, log.Ctx(jobCtx).Info()).Dur("durationMs", time.Since(startedAt)).Str("state", state).Str("reason", reason).Msg("job cancelled")
	} else if err != nil {
		state = "failed"
		if (w.Role == "scanner" || w.Role == "downloader") && j.Attempts < 3 && !errors.Is(err, media.ErrNeedsIdentification) && !errors.Is(err, media.ErrTMDBNotConfigured) {
			state = "pending"
		}
		message = "job_execution_failed"
		if j.Kind == "subtitle_auto" {
			// Daily/provider scheduling owns new attempts; reclaiming an expired
			// lease is still bounded by the common three-attempt job limit.
			state = "failed"
			message = "subtitle_download_failed"
			var pe *subtitleprovider.Error
			if errors.As(err, &pe) {
				state = "failed"
				message = pe.Code
			}
		}
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
		event := jobFields(jobCtx, observability.Error(log.Ctx(jobCtx).Error(), err)).Dur("durationMs", time.Since(startedAt)).Str("state", state).Bool("willRetry", state == "pending")
		if state == "pending" {
			event.Dur("retryInMs", time.Duration(min(300, 10*j.Attempts*j.Attempts))*time.Second)
		}
		event.Msg("job failed")
	}
	runState = state
	cancel()
	<-done
	finishCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	if _, err := w.DB.FinishJob(finishCtx, store.FinishJobParams{ID: j.ID, LeaseID: j.LeaseID, State: state, Error: message}); err != nil {
		runState = "result_unpersisted"
		if errors.Is(err, pgx.ErrNoRows) {
			runState = "interrupted"
		}
		observability.Error(jobFields(jobCtx, log.Ctx(jobCtx).Error()), err).Str("state", state).Msg("could not persist job result")
	} else if state == "completed" {
		jobFields(jobCtx, log.Ctx(jobCtx).Info()).Dur("durationMs", time.Since(startedAt)).Str("state", state).Msg("job completed")
	}
}
func (w *Worker) schedule(ctx context.Context) {
	ls, e := w.DB.DueLibraries(ctx)
	if e != nil {
		log.Ctx(ctx).Error().Err(e).Str("operation", "due_libraries").Msg("scan scheduling failed")
		return
	}
	for _, l := range ls {
		j, e := w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "scan", ResourceID: l.ID, Payload: []byte("{}")})
		if e != nil {
			log.Ctx(ctx).Error().Err(e).Str("libraryId", l.ID).Str("libraryName", l.Name).Msg("scheduled scan enqueue failed")
		} else {
			log.Ctx(ctx).Debug().Str("libraryId", l.ID).Str("libraryName", l.Name).Str("jobId", j.ID).Msg("scheduled scan queued")
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

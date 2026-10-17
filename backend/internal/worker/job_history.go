package worker

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"jfe/backend/internal/subtitleprovider"
)

// jobLogWriter isolates persistence from media processing. A bounded queue and
// batched writes prevent a slow database from holding up job leases.
type jobLogWriter struct {
	db       *store.Queries
	runID    string
	output   io.Writer
	level    zerolog.Level
	secrets  []string
	queue    chan json.RawMessage
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	accepted int
	dropped  int32
	reported bool
	terminal []json.RawMessage
}

var jobHistoryFields = strings.Fields("time level message error stage filename file processedFiles totalFiles skippedFiles durationMs queueWaitMs state reason willRetry retryInMs provider sqlState table constraint fileOperation exitCode jobId kind resourceId attempt workerRole workerId libraryId libraryName itemId title mediaKind fileId playbackId playbackMethod userId sourceId sourceTitle videoId videoTitle actorId pid videoAction audioAction subtitleAction progress phase operation startupMs ffmpegPid providerLeaseId providerLeaseUntil heldMs providerFileId candidateAttempt candidateLimit searchResults matchedCandidates subtitleSearchTitle subtitleSearchYear httpStatus lookupFailure preview videoCodec maxHeight maxBitrate errorCode")

var historyURL = regexp.MustCompile(`(?i)(?:https?|postgres(?:ql)?)://[^\s<>"']+`)
var historyCredential = regexp.MustCompile(`(?i)(?:bearer\s+\S+|(?:api[-_ ]?key|token|password|authorization)\s*[=:]\s*[^\s,;]+)`)

func newJobLogWriter(db *store.Queries, runID string, cfg config.Config) *jobLogWriter {
	level, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(cfg.LogLevel)))
	if err != nil {
		level = zerolog.InfoLevel
	}
	var output io.Writer = os.Stderr
	if strings.EqualFold(strings.TrimSpace(cfg.LogFormat), "console") {
		output = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339, NoColor: true}
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &jobLogWriter{db: db, runID: runID, output: output, level: level, ctx: ctx, cancel: cancel, queue: make(chan json.RawMessage, 256), done: make(chan struct{}), secrets: []string{cfg.DatabaseURL, cfg.TMDBToken, cfg.GeminiKey, cfg.OpenSubtitlesKey, cfg.OpenSubtitlesToken, cfg.OpenSubtitlesUsername, cfg.OpenSubtitlesPassword}}
	go writer.persist()
	return writer
}
func (w *jobLogWriter) clean(value string) string {
	for _, secret := range w.secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	value = historyURL.ReplaceAllString(value, "[redacted URL]")
	value = historyCredential.ReplaceAllString(value, "[redacted credential]")
	runes := []rune(value)
	if len(runes) > 2048 {
		value = string(runes[:2048]) + "..."
	}
	return value
}
func (w *jobLogWriter) sanitize(value any, depth int) any {
	if depth > 3 {
		return "[omitted]"
	}
	switch v := value.(type) {
	case string:
		return w.clean(v)
	case map[string]any:
		for k, item := range v {
			if k == "url" {
				if value, ok := item.(string); ok {
					// Provider URLs have a stricter public-route/query allowlist. Preserve
					// those diagnostics so admins can reproduce API failures from the UI.
					safe, _ := subtitleprovider.LogURL(value)
					v[k] = safe
					continue
				}
			}
			v[k] = w.sanitize(item, depth+1)
		}
		return v
	case []any:
		return "[omitted]"
	default:
		return value
	}
}
func (w *jobLogWriter) Write(data []byte) (int, error) { return w.WriteLevel(zerolog.InfoLevel, data) }
func (w *jobLogWriter) WriteLevel(level zerolog.Level, data []byte) (int, error) {
	if level >= w.level {
		_, _ = w.output.Write(data)
	}
	var event map[string]any
	if json.Unmarshal(data, &event) != nil {
		return len(data), nil
	}
	// Store operational fields only. Never persist arbitrary bodies, command
	// arguments, provider responses, job payloads or URLs added by future callers.
	safe := make(map[string]any, len(jobHistoryFields))
	for _, key := range jobHistoryFields {
		if value, ok := event[key]; ok {
			safe[key] = w.sanitize(value, 0)
		}
	}
	if provider, ok := safe["provider"].(map[string]any); ok {
		detail, _ := provider["detail"].(string)
		summary, _ := safe["error"].(string)
		if detail != "" && summary != "" {
			safe["error"] = w.clean(summary + ": " + detail)
		}
	}

	encoded, _ := json.Marshal(safe)
	message, _ := event["message"].(string)
	terminal := message == "job failed" || message == "job completed" || message == "job cancelled" || message == "could not persist job result"
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(data), nil
	}
	if terminal {
		w.terminal = append(w.terminal, encoded)
		return len(data), nil
	}
	if w.accepted >= 2000 {
		w.dropped++
		return len(data), nil
	}
	select {
	case w.queue <- encoded:
		w.accepted++
	default:
		w.dropped++
	}
	return len(data), nil
}
func (w *jobLogWriter) persist() {
	defer close(w.done)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]json.RawMessage, 0, 32)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		payload, _ := json.Marshal(batch)
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
		err := w.db.AppendJobLogs(ctx, store.AppendJobLogsParams{RunID: w.runID, Events: payload})
		cancel()
		if err != nil {
			w.mu.Lock()
			w.dropped += int32(len(batch))
			report := !w.reported
			w.reported = true
			w.mu.Unlock()
			if report {
				log.Error().Err(err).Str("runId", w.runID).Msg("job log persistence failed; some events are unavailable in UI")
			}
		}
		batch = batch[:0]
	}
	for {
		select {
		case data, ok := <-w.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, data)
			if len(batch) >= 32 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.ctx.Done():
			w.mu.Lock()
			w.dropped += int32(len(batch) + len(w.queue))
			w.mu.Unlock()
			return
		}
	}
}
func (w *jobLogWriter) close() int32 {
	w.mu.Lock()
	w.closed = true
	close(w.queue)
	w.mu.Unlock()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-w.done:
	case <-timer.C:
		w.cancel()
		<-w.done
	}
	w.cancel()
	// Final lifecycle events have separate capacity so a noisy scan cannot hide
	// its failure or completion behind a full progress queue.
	if len(w.terminal) > 0 {
		payload, _ := json.Marshal(w.terminal)
		ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
		e := w.db.AppendJobLogs(ctx, store.AppendJobLogsParams{RunID: w.runID, Events: payload})
		end()
		if e != nil {
			w.mu.Lock()
			w.dropped += int32(len(w.terminal))
			w.mu.Unlock()
			log.Error().Err(e).Str("runId", w.runID).Msg("could not persist final job logs")
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dropped
}

package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/store"
)

type jobTraceKey struct{}
type jobTrace struct {
	mu               sync.Mutex
	stage            string
	filename         string
	processed, total int
	skipped          int
	progressReported bool
}

func jobStage(ctx context.Context, stage string) {
	if trace, ok := ctx.Value(jobTraceKey{}).(*jobTrace); ok {
		trace.mu.Lock()
		trace.stage = stage
		trace.mu.Unlock()
	}
	jobFields(ctx, log.Ctx(ctx).Debug()).Msg("job stage started")
}
func jobFile(ctx context.Context, path string) {
	if trace, ok := ctx.Value(jobTraceKey{}).(*jobTrace); ok {
		trace.mu.Lock()
		trace.filename = filepath.Base(path)
		trace.mu.Unlock()
	}
}
func jobFields(ctx context.Context, event *zerolog.Event) *zerolog.Event {
	if trace, ok := ctx.Value(jobTraceKey{}).(*jobTrace); ok {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		event.Str("stage", trace.stage)
		if trace.filename != "" {
			event.Str("filename", trace.filename)
		}
		if trace.progressReported {
			event.Int("processedFiles", trace.processed).Int("totalFiles", trace.total)
			event.Int("skippedFiles", trace.skipped)
		}
	}
	return event
}
func jobProgress(ctx context.Context, done, total int) {
	if trace, ok := ctx.Value(jobTraceKey{}).(*jobTrace); ok {
		trace.mu.Lock()
		trace.processed = done
		trace.total = total
		trace.progressReported = true
		trace.mu.Unlock()
	}
}
func jobSkipped(ctx context.Context) {
	if trace, ok := ctx.Value(jobTraceKey{}).(*jobTrace); ok {
		trace.mu.Lock()
		trace.skipped++
		trace.mu.Unlock()
	}
}

func (w *Worker) jobLogger(ctx context.Context, j store.Job) zerolog.Logger {
	base := log.Ctx(ctx).With().Str("jobId", j.ID).Str("kind", j.Kind).Str("resourceId", j.ResourceID).Int32("attempt", j.Attempts).Logger()
	fields := base.With()
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	libraryID, itemID, fileID := "", "", ""
	switch j.Kind {
	case "scan":
		libraryID = j.ResourceID
	case "metadata":
		itemID = j.ResourceID
	case "subtitle_auto", "subtitle_prepare", "subtitle_sync":
		fileID = j.ResourceID
	case "subtitle_download":
		var p struct {
			FileID string `json:"fileId"`
			UserID string `json:"userId"`
		}
		if json.Unmarshal(j.Payload, &p) == nil {
			fileID = p.FileID
			fields = fields.Str("userId", p.UserID)
		}
	case "playback":
		fields = fields.Str("playbackId", j.ResourceID)
		if p, e := w.DB.GetPlayback(lookup, j.ResourceID); e == nil {
			fileID = p.FileID
			fields = fields.Str("userId", p.UserID).Str("playbackMethod", p.Method)
		}
	case "audio_tags":
		if p, e := w.DB.GetTagJob(lookup, j.ID); e == nil {
			fileID = p.FileID
		}
	case "youtube_preview", "youtube_download":
		fields = fields.Str("sourceId", j.ResourceID)
		if p, e := w.DB.GetImportSource(lookup, j.ResourceID); e == nil {
			libraryID = p.LibraryID
			fields = fields.Str("userId", p.UserID).Str("sourceTitle", p.Title)
		}
	}
	if fileID != "" {
		fields = fields.Str("fileId", fileID)
		if f, e := w.DB.GetFile(lookup, fileID); e == nil {
			itemID = f.ItemID
			fields = fields.Str("filename", filepath.Base(f.Path))
		}
	}
	if itemID != "" {
		fields = fields.Str("itemId", itemID)
		if i, e := w.DB.GetItem(lookup, itemID); e == nil {
			libraryID = i.LibraryID
			fields = fields.Str("title", i.Title).Str("mediaKind", i.Kind)
		}
	}
	if libraryID != "" {
		fields = fields.Str("libraryId", libraryID)
		if l, e := w.DB.GetLibrary(lookup, libraryID); e == nil {
			fields = fields.Str("libraryName", l.Name)
		}
	}
	return fields.Logger()
}

func (w *Worker) maintenance(ctx context.Context, operation string, fn func(context.Context) error) {
	if e := fn(ctx); e != nil && ctx.Err() == nil {
		log.Ctx(ctx).Error().Err(e).Str("operation", operation).Msg("worker maintenance failed")
	}
}

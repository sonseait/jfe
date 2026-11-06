package server

import (
	"context"
	"path/filepath"
	"time"

	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type SubtitleProviderDTO struct {
	Configured        bool      `json:"configured"`
	AccountConfigured bool      `json:"accountConfigured"`
	DownloadsEnabled  bool      `json:"downloadsEnabled"`
	ScannerReady      bool      `json:"scannerReady"`
	AutomaticReady    bool      `json:"automaticReady"`
	Status            string    `json:"status"`
	VIP               bool      `json:"vip"`
	Remaining         int       `json:"remaining"`
	ResetAt           time.Time `json:"resetAt"`
	RetryAt           time.Time `json:"retryAt"`
	CheckedAt         time.Time `json:"checkedAt"`
}
type AutoSubtitlePatch struct {
	Enabled bool `json:"enabled"`
}
type AutoSubtitleQuery struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"maxLength=100"`
}
type AutoSubtitleDTO struct {
	FileID         string    `json:"fileId"`
	ItemID         string    `json:"itemId"`
	Title          string    `json:"title"`
	Filename       string    `json:"filename"`
	State          string    `json:"state"`
	ErrorCode      string    `json:"errorCode"`
	ProviderFileID int       `json:"providerFileId"`
	NextSearchAt   time.Time `json:"nextSearchAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
type AutoSubtitlesDTO struct {
	Enabled    bool              `json:"enabled"`
	Total      int64             `json:"total"`
	Missing    int64             `json:"missing"`
	Items      []AutoSubtitleDTO `json:"items"`
	NextCursor string            `json:"nextCursor"`
}

func (s *Server) autoSubtitles(ctx context.Context, id, cursor string) (AutoSubtitlesDTO, error) {
	result := AutoSubtitlesDTO{Items: []AutoSubtitleDTO{}}
	l, e := s.DB.GetLibrary(ctx, id)
	if e != nil {
		return result, e
	}
	if l.Kind != "movies" && l.Kind != "series" {
		return result, route.Fail(422, "Subtitles require a video library")
	}
	result.Enabled = l.AutoSubtitles
	counts, e := s.DB.AutoSubtitleCounts(ctx, id)
	if e != nil {
		return result, e
	}
	result.Total = counts.Total
	result.Missing = counts.Missing
	rows, e := s.DB.ListAutoSubtitles(ctx, store.ListAutoSubtitlesParams{LibraryID: id, AfterID: cursor})
	if e != nil {
		return result, e
	}
	if len(rows) > 100 {
		rows = rows[:100]
		result.NextCursor = rows[99].FileID
	}
	for _, r := range rows {
		state := r.State
		if r.JobState == "pending" {
			state = "pending"
		}
		if r.JobState == "running" && state != "downloading" {
			state = "searching"
		}
		result.Items = append(result.Items, AutoSubtitleDTO{FileID: r.FileID, ItemID: r.ItemID, Title: r.Title, Filename: filepath.Base(r.Path), State: state, ErrorCode: r.ErrorCode, ProviderFileID: int(r.ProviderFileID), NextSearchAt: r.NextSearchAt, UpdatedAt: r.UpdatedAt})
	}
	return result, nil
}

func (s *Server) autoSubtitleRoutes() {
	route.Get(s.Routes, op("/admin/subtitle-provider", "getSubtitleProvider", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[SubtitleProviderDTO], error) {
		state, e := s.DB.GetSubtitleProviderState(ctx)
		ready, readyErr := s.DB.OpenSubtitlesWorkerReady(ctx)
		if e == nil {
			e = readyErr
		}
		autoReady, autoErr := s.DB.AutoSubtitlesWorkerReady(ctx)
		if e == nil {
			e = autoErr
		}
		return out(SubtitleProviderDTO{AutomaticReady: s.Config.SubtitleDownloads && s.Config.OpenSubtitlesKey != "" && autoReady, Configured: s.Config.OpenSubtitlesKey != "", AccountConfigured: s.Config.OpenSubtitlesToken != "" || (s.Config.OpenSubtitlesUsername != "" && s.Config.OpenSubtitlesPassword != ""), DownloadsEnabled: s.Config.SubtitleDownloads, ScannerReady: ready, Status: state.Status, VIP: state.Vip, Remaining: int(state.Remaining), ResetAt: state.ResetAt, RetryAt: state.RetryAt, CheckedAt: state.CheckedAt}, e)
	})
	route.Post(s.Routes, op("/admin/subtitle-provider/check", "checkSubtitleProvider", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[JobDTO], error) {
		if !s.openSubtitlesAvailable(ctx) {
			return out(JobDTO{}, route.Fail(503, "OpenSubtitles scanner is unavailable"))
		}
		job, e := s.enqueue(ctx, "scanner", "subtitle_provider_check", "opensubtitles", nil)
		return out(job, e)
	})
	route.Get(s.Routes, op("/libraries/:id/automatic-subtitles", "getAutomaticSubtitles", "admin"), func(ctx context.Context, in route.Input[route.Empty, AutoSubtitleQuery, IDParams]) (route.Output[AutoSubtitlesDTO], error) {
		result, e := s.autoSubtitles(ctx, in.Params.ID, in.Query.Cursor)
		return out(result, e)
	})
	route.Patch(s.Routes, op("/libraries/:id/automatic-subtitles", "setAutomaticSubtitles", "admin"), func(ctx context.Context, in route.Input[AutoSubtitlePatch, route.Empty, IDParams]) (route.Output[AutoSubtitlesDTO], error) {
		if in.Body.Enabled {
			ready, e := s.DB.AutoSubtitlesWorkerReady(ctx)
			if e != nil {
				return out(AutoSubtitlesDTO{}, e)
			}
			if !s.Config.SubtitleDownloads || s.Config.OpenSubtitlesKey == "" || !ready {
				return out(AutoSubtitlesDTO{}, route.Fail(503, "Automatic subtitle downloads are unavailable"))
			}
		}
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		defer tx.Rollback(ctx)
		q := s.DB.WithTx(tx)
		if e = q.LockSubtitleLibrary(ctx, in.Params.ID); e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		l, e := q.LockAutoSubtitleLibrary(ctx, in.Params.ID)
		if e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		if l.Kind != "movies" && l.Kind != "series" {
			return out(AutoSubtitlesDTO{}, route.Fail(422, "Subtitles require a video library"))
		}
		if e = q.SetAutoSubtitleLibrary(ctx, store.SetAutoSubtitleLibraryParams{ID: l.ID, AutoSubtitles: in.Body.Enabled}); e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		if in.Body.Enabled {
			if e = q.InitializeAutoSubtitles(ctx); e != nil {
				return out(AutoSubtitlesDTO{}, e)
			}
			if e = q.RetryLibrarySubtitles(ctx, l.ID); e != nil {
				return out(AutoSubtitlesDTO{}, e)
			}
		}
		if e = tx.Commit(ctx); e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		result, e := s.autoSubtitles(ctx, l.ID, "")
		return out(result, e)
	})
	route.Post(s.Routes, op("/libraries/:id/automatic-subtitles/search", "searchAutomaticSubtitles", "admin"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[AutoSubtitlesDTO], error) {
		l, e := s.DB.GetLibrary(ctx, in.Params.ID)
		if e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		ready, e := s.DB.AutoSubtitlesWorkerReady(ctx)
		if e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		if !l.AutoSubtitles || !s.Config.SubtitleDownloads || !ready {
			return out(AutoSubtitlesDTO{}, route.Fail(409, "Enable automatic subtitles with an available scanner first"))
		}
		if e = s.DB.InitializeAutoSubtitles(ctx); e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		if e = s.DB.RetryLibrarySubtitles(ctx, l.ID); e != nil {
			return out(AutoSubtitlesDTO{}, e)
		}
		result, e := s.autoSubtitles(ctx, l.ID, "")
		return out(result, e)
	})
}

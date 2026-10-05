package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type SubtitleSourceDTO struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Codec     string `json:"codec"`
	CanSync   bool   `json:"canSync"`
	CanDelete bool   `json:"canDelete"`
	Reason    string `json:"reason"`
}
type SubtitleSourcesDTO struct {
	Revision string              `json:"revision"`
	CanEdit  bool                `json:"canEdit"`
	Reason   string              `json:"reason"`
	Items    []SubtitleSourceDTO `json:"items"`
}
type SubtitlePrepareRequest struct {
	Revision   string `json:"revision" jsonschema:"minLength=64,maxLength=64"`
	TrackIndex int    `json:"trackIndex" jsonschema:"minimum=0"`
}
type SubtitleMutationRequest struct {
	PreparationID string `json:"preparationId" jsonschema:"format=uuid"`
	Fingerprint   string `json:"fingerprint" jsonschema:"minLength=64,maxLength=64"`
	OffsetMS      int    `json:"offsetMs" jsonschema:"minimum=-600000,maximum=600000"`
}
type SubtitleSyncJobParams struct {
	ID    string `json:"id" jsonschema:"format=uuid"`
	JobID string `json:"jobId" jsonschema:"format=uuid"`
}
type SubtitleSyncJobDTO struct {
	Job         JobDTO      `json:"job"`
	Phase       string      `json:"phase"`
	ErrorCode   string      `json:"errorCode"`
	Fingerprint string      `json:"fingerprint"`
	Cues        []media.Cue `json:"cues"`
}

func (s *Server) subtitleEditAccess(ctx context.Context, id string) (store.MediaFile, error) {
	f, e := s.DB.GetFile(ctx, id)
	if e != nil {
		return f, e
	}
	i, e := s.authorizedItem(ctx, f.ItemID)
	if e != nil {
		return f, e
	}
	ok, e := s.DB.CanEditSubtitles(ctx, store.CanEditSubtitlesParams{LibraryID: i.LibraryID, UserID: route.User(ctx).ID})
	if e != nil {
		return f, e
	}
	if !ok {
		return f, route.Fail(403, "Subtitle editing permission required")
	}
	return f, nil
}
func (s *Server) subtitleSources(ctx context.Context, id string) (SubtitleSourcesDTO, error) {
	v := SubtitleSourcesDTO{Items: []SubtitleSourceDTO{}}
	if e := s.subtitleAccess(ctx, id); e != nil {
		return v, e
	}
	f, e := s.subtitleEditAccess(ctx, id)
	if e != nil {
		if p, ok := e.(*route.Problem); ok && p.Status == 403 {
			v.Reason = "permission"
			return v, nil
		}
		return v, e
	}
	if !s.Config.SubtitleEditing {
		v.Reason = "disabled"
		return v, nil
	}
	ready, e := s.DB.SubtitleSyncWorkerReady(ctx)
	if e != nil {
		return v, e
	}
	if !ready {
		v.Reason = "worker_unavailable"
		return v, nil
	}
	v.CanEdit = true
	v.Revision = media.SubtitleProbeRevision(f.Probe)
	mkv, e := s.DB.SubtitleMKVWorkerReady(ctx)
	if e != nil {
		return v, e
	}
	var p media.Probe
	if e = json.Unmarshal(f.Probe, &p); e != nil {
		return v, e
	}
	for _, t := range p.Streams {
		if t.Type != "subtitle" {
			continue
		}
		kind := "embedded"
		supported := strings.EqualFold(filepath.Ext(f.Path), ".mkv") && mkv
		if t.ExternalPath != "" {
			kind = "sidecar"
			supported = media.SubtitleSidecarSource(f.Path, t.ExternalPath)
		}
		reason := ""
		if !supported {
			reason = "unsupported_container"
			if kind == "sidecar" {
				reason = "subtitle_unsupported_format"
			}
			if kind == "embedded" && strings.EqualFold(filepath.Ext(f.Path), ".mkv") && !mkv {
				reason = "mkv_unavailable"
			}
		} else if !media.SubtitleTextCodec(t.Codec) {
			reason = "subtitle_unsupported_format"
			if t.SubtitleType() == "bitmap" {
				reason = "bitmap"
			}
		}
		v.Items = append(v.Items, SubtitleSourceDTO{Index: t.Index, Name: strings.TrimSpace(t.Tags["language"] + " " + t.Tags["title"] + " " + t.Codec), Kind: kind, Codec: t.Codec, CanSync: supported && media.SubtitleTextCodec(t.Codec), CanDelete: supported, Reason: reason})
	}
	return v, nil
}
func (s *Server) enqueueSubtitle(ctx context.Context, file string, track int, action string, prepared *store.SubtitleSyncJob, offset int, revision string) (JobDTO, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return JobDTO{}, e
	}
	defer tx.Rollback(ctx)
	q := s.DB.WithTx(tx)
	f, e := q.GetFile(ctx, file)
	if e != nil {
		return JobDTO{}, e
	}
	item, e := q.GetItem(ctx, f.ItemID)
	if e != nil {
		return JobDTO{}, e
	}
	if e = q.LockSubtitleLibrary(ctx, item.LibraryID); e != nil {
		return JobDTO{}, e
	}
	if _, e = q.GetFile(ctx, file); e != nil {
		return JobDTO{}, e
	}
	allowed, e := q.CanEditSubtitles(ctx, store.CanEditSubtitlesParams{LibraryID: item.LibraryID, UserID: route.User(ctx).ID})
	if e != nil {
		return JobDTO{}, e
	}
	if !allowed {
		return JobDTO{}, route.Fail(403, "Subtitle editing permission required")
	}
	if e = q.LockSubtitleFile(ctx, file); e != nil {
		return JobDTO{}, e
	}
	f, e = q.GetFile(ctx, file)
	if e != nil {
		return JobDTO{}, e
	}
	if revision != "" && revision != media.SubtitleProbeRevision(f.Probe) {
		return JobDTO{}, route.Fail(409, "subtitle_changed")
	}
	if prepared != nil {
		revision = prepared.ProbeRevision
		if revision != media.SubtitleProbeRevision(f.Probe) {
			return JobDTO{}, route.Fail(409, "subtitle_changed")
		}
	}

	busy, e := q.SubtitleFileBusy(ctx, file)
	if e != nil {
		return JobDTO{}, e
	}
	if busy {
		return JobDTO{}, route.Fail(409, "subtitle_busy")
	}
	if action != "prepare" {
		busy, e = q.SubtitlePlaybackBusy(ctx, file)
		if e != nil {
			return JobDTO{}, e
		}
		if busy {
			return JobDTO{}, route.Fail(409, "subtitle_playback_busy")
		}
		busy, e = q.SubtitleTranscoderBusy(ctx, file)
		if e != nil {
			return JobDTO{}, e
		}
		if busy {
			return JobDTO{}, route.Fail(409, "subtitle_playback_busy")
		}
	}
	id := uuid.NewString()
	j, e := q.Enqueue(ctx, store.EnqueueParams{ID: id, Role: "scanner", Kind: "subtitle_sync", ResourceID: file, Payload: []byte("{}")})
	if e != nil {
		return JobDTO{}, e
	}
	if j.ID != id {
		return JobDTO{}, route.Fail(409, "subtitle_busy")
	}
	fingerprint := ""
	if prepared != nil {
		fingerprint = prepared.Fingerprint
	}
	e = q.CreateSubtitleSyncJob(ctx, store.CreateSubtitleSyncJobParams{ID: id, UserID: route.User(ctx).ID, FileID: file, TrackIndex: int32(track), Action: action, Fingerprint: fingerprint, OffsetMs: int32(offset), ProbeRevision: revision})
	if e != nil {
		return JobDTO{}, e
	}
	if prepared != nil {
		e = q.PrepareSubtitleSyncJob(ctx, store.PrepareSubtitleSyncJobParams{ID: id, Fingerprint: fingerprint, SourcePath: prepared.SourcePath, Cues: prepared.Cues})
		if e != nil {
			return JobDTO{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return JobDTO{}, e
	}
	return jobDTO(j), nil
}
func (s *Server) subtitleSyncRoutes() {
	r := s.Routes
	route.Get(r, op("/files/:id/subtitle-sync", "subtitleSyncSources", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[SubtitleSourcesDTO], error) {
		v, e := s.subtitleSources(ctx, in.Params.ID)
		return out(v, e)
	})
	route.Post(r, op("/files/:id/subtitle-sync/prepare", "prepareSubtitleSync", "user"), func(ctx context.Context, in route.Input[SubtitlePrepareRequest, route.Empty, IDParams]) (route.Output[JobDTO], error) {
		if _, e := s.subtitleEditAccess(ctx, in.Params.ID); e != nil {
			return out(JobDTO{}, e)
		}
		sources, e := s.subtitleSources(ctx, in.Params.ID)
		if e != nil {
			return out(JobDTO{}, e)
		}
		if sources.Revision != in.Body.Revision {
			return out(JobDTO{}, route.Fail(409, "subtitle_changed"))
		}
		for _, source := range sources.Items {
			if source.Index == in.Body.TrackIndex && source.CanDelete {
				v, e := s.enqueueSubtitle(ctx, in.Params.ID, source.Index, "prepare", nil, 0, in.Body.Revision)
				return out(v, e)
			}
		}
		return out(JobDTO{}, route.Fail(409, "Subtitle source unavailable"))
	})
	route.Get(r, op("/files/:id/subtitle-sync/jobs/:jobId", "getSubtitleSyncJob", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, SubtitleSyncJobParams]) (route.Output[SubtitleSyncJobDTO], error) {
		v := SubtitleSyncJobDTO{Cues: []media.Cue{}}
		if _, e := s.subtitleEditAccess(ctx, in.Params.ID); e != nil {
			return out(v, e)
		}
		record, e := s.DB.GetSubtitleSyncJob(ctx, in.Params.JobID)
		if e != nil {
			return out(v, e)
		}
		if record.FileID != in.Params.ID || record.UserID != route.User(ctx).ID {
			return out(v, route.Fail(404, "Not found"))
		}
		j, e := s.DB.GetJob(ctx, record.ID)
		if e != nil {
			return out(v, e)
		}
		v.Job = jobDTO(j)
		v.Phase = record.Phase
		v.ErrorCode = record.ErrorCode
		if j.State == "completed" {
			v.Fingerprint = record.Fingerprint
			e = json.Unmarshal(record.Cues, &v.Cues)
		}
		return out(v, e)
	})
	for _, action := range []string{"save", "delete"} {
		route.Post(r, op("/files/:id/subtitle-sync/"+action, action+"SubtitleSource", "user"), func(ctx context.Context, in route.Input[SubtitleMutationRequest, route.Empty, IDParams]) (route.Output[JobDTO], error) {
			if _, e := s.subtitleEditAccess(ctx, in.Params.ID); e != nil {
				return out(JobDTO{}, e)
			}
			p, e := s.DB.GetSubtitleSyncJob(ctx, in.Body.PreparationID)
			if e != nil {
				return out(JobDTO{}, e)
			}
			if p.FileID != in.Params.ID || p.UserID != route.User(ctx).ID || p.Action != "prepare" || p.Phase != "ready" || p.Fingerprint != in.Body.Fingerprint {
				return out(JobDTO{}, route.Fail(409, "subtitle_changed"))
			}
			if action == "save" && string(p.Cues) == "[]" {
				return out(JobDTO{}, route.Fail(409, "subtitle_unsupported_format"))
			}
			j, e := s.DB.GetJob(ctx, p.ID)
			if e != nil {
				return out(JobDTO{}, e)
			}
			if j.State != "completed" {
				return out(JobDTO{}, route.Fail(409, "subtitle_not_ready"))
			}
			sources, e := s.subtitleSources(ctx, in.Params.ID)
			if e != nil {
				return out(JobDTO{}, e)
			}
			if p.ProbeRevision != sources.Revision {
				return out(JobDTO{}, route.Fail(409, "subtitle_changed"))
			}
			for _, source := range sources.Items {
				if source.Index == int(p.TrackIndex) && ((action == "save" && source.CanSync) || (action == "delete" && source.CanDelete)) {
					offset := in.Body.OffsetMS
					if action == "delete" {
						offset = 0
					}
					v, e := s.enqueueSubtitle(ctx, p.FileID, int(p.TrackIndex), action, &p, offset, "")
					return out(v, e)
				}
			}
			return out(JobDTO{}, route.Fail(409, "Subtitle source unavailable"))
		})
	}
	route.Delete(r, op("/files/:id/subtitles/:subtitleId", "deleteUploadedSubtitle", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, SubtitleParams]) (route.Output[HealthDTO], error) {
		if e := s.subtitleAccess(ctx, in.Params.ID); e != nil {
			return out(HealthDTO{}, e)
		}
		_, e := s.DB.DeleteUploadedSubtitle(ctx, store.DeleteUploadedSubtitleParams{ID: in.Params.SubtitleID, FileID: in.Params.ID, UserID: route.User(ctx).ID})
		return out(HealthDTO{"ok"}, e)
	})
}

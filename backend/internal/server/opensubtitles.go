package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
	"jfe/backend/internal/subtitleprovider"
)

type OpenSubtitlesQuery struct {
	Search   string `query:"search" json:"search,omitempty" jsonschema:"maxLength=200"`
	Language string `query:"language" json:"language" jsonschema:"enum=vi,enum=en"`
}
type OpenSubtitleDTO struct {
	FileID          int    `json:"fileId"`
	Name            string `json:"name"`
	Language        string `json:"language"`
	Release         string `json:"release"`
	Downloads       int    `json:"downloads"`
	HearingImpaired bool   `json:"hearingImpaired"`
}
type OpenSubtitlesDTO struct {
	Items []OpenSubtitleDTO `json:"items"`
}
type OpenSubtitleRequest struct {
	FileID int `json:"fileId" jsonschema:"minimum=1"`
}
type OpenSubtitleJobParams struct {
	ID    string `json:"id" jsonschema:"format=uuid"`
	JobID string `json:"jobId" jsonschema:"format=uuid"`
}
type OpenSubtitleDownloadDTO struct {
	JobID      string `json:"jobId"`
	SubtitleID string `json:"subtitleId"`
	State      string `json:"state"`
}
type openSubtitlePayload struct {
	UserID         string `json:"userId"`
	FileID         string `json:"fileId"`
	ProviderFileID int    `json:"providerFileId"`
}

func (s *Server) openSubtitlesAvailable(ctx context.Context) bool {
	if s.Config.OpenSubtitlesKey == "" || s.DB == nil {
		return false
	}
	ready, err := s.DB.OpenSubtitlesWorkerReady(ctx)
	return err == nil && ready
}

func (s *Server) openSubtitleRoutes() {
	route.Get(s.Routes, op("/files/:id/opensubtitles", "searchOpenSubtitles", "user"), func(ctx context.Context, in route.Input[route.Empty, OpenSubtitlesQuery, IDParams]) (route.Output[OpenSubtitlesDTO], error) {
		v := OpenSubtitlesDTO{Items: []OpenSubtitleDTO{}}
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(v, err)
		}
		if !s.openSubtitlesAvailable(ctx) {
			return out(v, route.Fail(503, "OpenSubtitles scanner is unavailable"))
		}
		f, err := s.DB.GetFile(ctx, in.Params.ID)
		if err != nil {
			return out(v, err)
		}
		if !f.Available {
			return out(v, route.Fail(404, "Media file unavailable"))
		}
		i, err := s.DB.GetItem(ctx, f.ItemID)
		if err != nil {
			return out(v, err)
		}
		q := url.Values{"languages": {in.Query.Language}, "order_by": {"download_count"}, "order_direction": {"desc"}}
		if in.Query.Search != "" {
			q.Set("query", in.Query.Search)
		} else if i.Kind == "episode" {
			parent, err := s.DB.GetItem(ctx, i.ParentID)
			if err != nil {
				return out(v, err)
			}
			if parent.ProviderID != "" {
				q.Set("parent_tmdb_id", parent.ProviderID)
			} else {
				q.Set("query", parent.Title)
			}
			q.Set("season_number", strconv.Itoa(int(i.Season)))
			q.Set("episode_number", strconv.Itoa(int(i.Episode)))
		} else if i.Kind == "movie" {
			q.Set("type", "movie")
			if i.ProviderID != "" {
				q.Set("tmdb_id", i.ProviderID)
			} else {
				q.Set("query", i.Title)
			}
			if i.Year > 0 {
				q.Set("year", strconv.Itoa(int(i.Year)))
			}
		} else {
			return out(v, route.Fail(422, "Subtitles require a movie or episode"))
		}
		items, err := (subtitleprovider.Client{Key: s.Config.OpenSubtitlesKey, Token: s.Config.OpenSubtitlesToken}).Search(ctx, q)
		if err != nil {
			return out(v, route.Fail(503, "OpenSubtitles search failed; check provider credentials and quota"))
		}
		for _, m := range items {
			v.Items = append(v.Items, OpenSubtitleDTO{m.FileID, m.Name, m.Language, m.Release, m.Downloads, m.HearingImpaired})
		}
		return out(v, nil)
	})
	route.Post(s.Routes, op("/files/:id/opensubtitles/download", "downloadOpenSubtitle", "user"), func(ctx context.Context, in route.Input[OpenSubtitleRequest, route.Empty, IDParams]) (route.Output[OpenSubtitleDownloadDTO], error) {
		v := OpenSubtitleDownloadDTO{}
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(v, err)
		}
		if !s.openSubtitlesAvailable(ctx) {
			return out(v, route.Fail(503, "OpenSubtitles scanner is unavailable"))
		}
		f, err := s.DB.GetFile(ctx, in.Params.ID)
		if err != nil {
			return out(v, err)
		}
		if !f.Available {
			return out(v, route.Fail(404, "Media file unavailable"))
		}
		user := route.User(ctx).ID
		i, err := s.DB.GetItem(ctx, f.ItemID)
		if err != nil {
			return out(v, err)
		}
		if i.Kind != "movie" && i.Kind != "episode" {
			return out(v, route.Fail(422, "Subtitles require a movie or episode"))
		}
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("opensubtitles\x00"+user+"\x00"+f.ID+"\x00"+strconv.Itoa(in.Body.FileID))).String()
		if _, err := s.DB.GetSubtitle(ctx, store.GetSubtitleParams{ID: id, FileID: f.ID, UserID: user}); err == nil {
			return out(OpenSubtitleDownloadDTO{SubtitleID: id, State: "completed"}, nil)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return out(v, err)
		}
		payload, err := json.Marshal(openSubtitlePayload{user, f.ID, in.Body.FileID})
		if err != nil {
			return out(v, err)
		}
		j, err := s.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "subtitle_download", ResourceID: id, Payload: payload})
		return out(OpenSubtitleDownloadDTO{j.ID, id, j.State}, err)
	})
	route.Get(s.Routes, op("/files/:id/opensubtitles/jobs/:jobId", "getOpenSubtitleDownload", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, OpenSubtitleJobParams]) (route.Output[OpenSubtitleDownloadDTO], error) {
		v := OpenSubtitleDownloadDTO{}
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(v, err)
		}
		j, err := s.DB.GetJob(ctx, in.Params.JobID)
		if err != nil {
			return out(v, err)
		}
		var p openSubtitlePayload
		if json.Unmarshal(j.Payload, &p) != nil || j.Kind != "subtitle_download" || p.UserID != route.User(ctx).ID || p.FileID != in.Params.ID {
			return out(v, route.Fail(404, "Subtitle job unavailable"))
		}
		return out(OpenSubtitleDownloadDTO{j.ID, j.ResourceID, j.State}, nil)
	})
}

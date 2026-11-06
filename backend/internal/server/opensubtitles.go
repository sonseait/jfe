package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
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
	Items       []OpenSubtitleDTO `json:"items"`
	SearchTitle string            `json:"searchTitle"`
	SearchYear  int               `json:"searchYear"`
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
		if i.Kind != "movie" && i.Kind != "episode" {
			return out(v, route.Fail(422, "Subtitles require a movie or episode"))
		}
		target := subtitleprovider.SearchTarget{Title: i.Title, Year: int(i.Year), Kind: i.Kind, Season: int(i.Season), Episode: int(i.Episode)}
		target.TMDBID, _ = strconv.Atoi(i.ProviderID)
		if i.Kind == "episode" {
			parent, err := s.DB.GetItem(ctx, i.ParentID)
			if err != nil {
				return out(v, err)
			}
			target.Title, target.Year = parent.Title, int(parent.Year)
			target.TMDBID, _ = strconv.Atoi(parent.ProviderID)
		}
		target = target.WithFilenameFallback(f.Path)
		search := strings.TrimSpace(in.Query.Search)
		if search == "" {
			target = subtitleprovider.ResolveSearchTarget(ctx, s.Config.TMDBToken, target)
		}
		q := target.Query(in.Query.Language, search)
		if q.Get("query") == "" {
			return out(v, route.Fail(422, "A movie or series title is required for subtitle search"))
		}
		v.SearchTitle = q.Get("query")
		v.SearchYear, _ = strconv.Atoi(q.Get("year"))
		log.Ctx(ctx).Info().Str("subtitleSearchTitle", q.Get("query")).Str("subtitleSearchYear", q.Get("year")).Msg("Searching OpenSubtitles using resolved title")
		var items []subtitleprovider.Candidate
		err = subtitleprovider.Do(ctx, s.Pool, s.Config, func(ctx context.Context, client *subtitleprovider.Client) error {
			var e error
			items, e = client.Search(ctx, q)
			return e
		})
		if err != nil {
			// Preserve both the public HTTP problem and the original typed cause.
			// The completion event must retain URL/timing diagnostics at error level.
			return out(v, fmt.Errorf("%w: %w", route.Fail(503, "OpenSubtitles search failed; check provider credentials and quota"), err))
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
		if err == nil {
			logQueued(ctx, jobDTO(j))
		}
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

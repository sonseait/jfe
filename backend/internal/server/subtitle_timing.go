package server

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type SubtitleTimingDTO struct {
	Revision string         `json:"revision"`
	Offsets  map[string]int `json:"offsets"`
}
type SubtitleTimingRequest struct {
	Revision string `json:"revision" jsonschema:"minLength=64,maxLength=64"`
	Subtitle string `json:"subtitle" jsonschema:"minLength=1,maxLength=64"`
	OffsetMS int    `json:"offsetMs" jsonschema:"minimum=-600000,maximum=600000"`
}

// Both the text overlay and embedded burn-in selection identify the same source.
// Probe revisions invalidate timings after source edits or track renumbering.
type subtitleTimingSourceList struct {
	Choices  map[string]string
	Revision string
}

func (s *Server) subtitleTimingSources(ctx context.Context, fileID string) (*subtitleTimingSourceList, error) {
	if err := s.subtitleAccess(ctx, fileID); err != nil {
		return nil, err
	}
	f, err := s.DB.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	var probe media.Probe
	if err = json.Unmarshal(f.Probe, &probe); err != nil {
		return nil, err
	}
	sources := map[string]string{}
	revision := media.SubtitleProbeRevision(f.Probe)
	for _, track := range probe.Streams {
		if track.Type != "subtitle" {
			continue
		}
		choice := strconv.Itoa(track.Index)
		key := "track:" + revision + ":" + choice
		sources[choice] = key
		if track.SubtitleID != "" {
			sources["upload:"+track.SubtitleID] = key
		}
	}
	uploads, err := s.DB.ListSubtitles(ctx, store.ListSubtitlesParams{FileID: fileID, UserID: route.User(ctx).ID})
	if err != nil {
		return nil, err
	}
	for _, upload := range uploads {
		sources["upload:"+upload.ID] = "upload:" + upload.ID
	}
	return &subtitleTimingSourceList{Choices: sources, Revision: revision}, nil
}
func (s *Server) subtitleTiming(ctx context.Context, fileID string, sources *subtitleTimingSourceList) (SubtitleTimingDTO, error) {
	v := SubtitleTimingDTO{Offsets: map[string]int{}}
	v.Revision = sources.Revision
	rows, err := s.DB.ListPersonalSubtitleTiming(ctx, store.ListPersonalSubtitleTimingParams{UserID: route.User(ctx).ID, FileID: fileID})
	if err != nil {
		return v, err
	}
	offsets := map[string]int{}
	for _, row := range rows {
		offsets[row.SourceKey] = int(row.OffsetMs)
	}
	for choice, key := range sources.Choices {
		if value, ok := offsets[key]; ok {
			v.Offsets[choice] = value
		}
	}
	return v, nil
}
func (s *Server) subtitleTimingRoutes() {
	route.Get(s.Routes, op("/files/:id/subtitle-timing", "getPersonalSubtitleTiming", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[SubtitleTimingDTO], error) {
		sources, err := s.subtitleTimingSources(ctx, in.Params.ID)
		if err != nil {
			return out(SubtitleTimingDTO{}, err)
		}
		v, err := s.subtitleTiming(ctx, in.Params.ID, sources)
		return out(v, err)
	})
	route.Put(s.Routes, op("/files/:id/subtitle-timing", "savePersonalSubtitleTiming", "user"), func(ctx context.Context, in route.Input[SubtitleTimingRequest, route.Empty, IDParams]) (route.Output[SubtitleTimingDTO], error) {
		sources, err := s.subtitleTimingSources(ctx, in.Params.ID)
		if err != nil {
			return out(SubtitleTimingDTO{}, err)
		}
		key, ok := sources.Choices[in.Body.Subtitle]
		if !ok || strings.TrimSpace(key) == "" {
			return out(SubtitleTimingDTO{}, route.Fail(404, "Subtitle unavailable"))
		}
		if strings.HasPrefix(key, "track:") && !strings.HasPrefix(key, "track:"+in.Body.Revision+":") {
			return out(SubtitleTimingDTO{}, route.Fail(409, "subtitle_changed"))
		}
		user := route.User(ctx).ID
		if in.Body.OffsetMS == 0 {
			err = s.DB.DeletePersonalSubtitleTiming(ctx, store.DeletePersonalSubtitleTimingParams{UserID: user, FileID: in.Params.ID, SourceKey: key})
		} else {
			err = s.DB.SavePersonalSubtitleTiming(ctx, store.SavePersonalSubtitleTimingParams{UserID: user, FileID: in.Params.ID, SourceKey: key, OffsetMs: int32(in.Body.OffsetMS)})
		}
		if err != nil {
			return out(SubtitleTimingDTO{}, err)
		}
		v, err := s.subtitleTiming(ctx, in.Params.ID, sources)
		return out(v, err)
	})
}

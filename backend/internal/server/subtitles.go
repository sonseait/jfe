package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type SubtitleUpload struct {
	Name    string `json:"name" jsonschema:"minLength=1,maxLength=255"`
	Content string `json:"content" jsonschema:"minLength=1,maxLength=524288"`
}
type SubtitleDTO struct {
	Uploaded bool   `json:"uploaded,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	CueCount int    `json:"cueCount"`
}
type SubtitlesDTO struct {
	Items []SubtitleDTO `json:"items"`
}
type SubtitleDocument struct {
	Cues []media.Cue `json:"cues"`
}
type SubtitleParams struct {
	ID         string `json:"id" jsonschema:"format=uuid"`
	SubtitleID string `json:"subtitleId" jsonschema:"format=uuid"`
}

func (s *Server) subtitleAccess(ctx context.Context, fileID string) error {
	f, err := s.DB.GetFile(ctx, fileID)
	if err != nil {
		return err
	}
	_, err = s.authorizedItem(ctx, f.ItemID)
	return err
}

func (s *Server) subtitleRoutes() {
	route.Get(s.Routes, op("/files/:id/subtitles", "listSubtitles", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[SubtitlesDTO], error) {
		v := SubtitlesDTO{Items: []SubtitleDTO{}}
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(v, err)
		}
		f, err := s.DB.GetFile(ctx, in.Params.ID)
		if err != nil {
			return out(v, err)
		}
		var probe media.Probe
		_ = json.Unmarshal(f.Probe, &probe)
		for _, track := range probe.Streams {
			if track.SubtitleID != "" {
				v.Items = append(v.Items, SubtitleDTO{ID: track.SubtitleID, Name: strings.TrimSpace(track.Tags["language"] + " " + track.Tags["title"] + " " + track.Codec)})
			}
		}
		rows, err := s.DB.ListSubtitles(ctx, store.ListSubtitlesParams{FileID: in.Params.ID, UserID: route.User(ctx).ID})
		for _, row := range rows {
			v.Items = append(v.Items, SubtitleDTO{ID: row.ID, Name: row.Name, CueCount: int(row.CueCount), Uploaded: true})
		}
		return out(v, err)
	})
	route.Post(s.Routes, op("/files/:id/subtitles", "uploadSubtitle", "user"), func(ctx context.Context, in route.Input[SubtitleUpload, route.Empty, IDParams]) (route.Output[SubtitleDTO], error) {
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(SubtitleDTO{}, err)
		}
		name := filepath.Base(strings.ReplaceAll(in.Body.Name, "\\", "/"))
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".srt" && ext != ".vtt" {
			return out(SubtitleDTO{}, route.Fail(422, "Use UTF-8 SRT or WebVTT subtitles"))
		}
		cues, err := media.ParseSubtitles(in.Body.Content)
		if err != nil {
			return out(SubtitleDTO{}, route.Fail(422, "Invalid subtitles; use UTF-8 SRT or WebVTT up to 512 KiB"))
		}
		data, err := json.Marshal(cues)
		if err != nil {
			return out(SubtitleDTO{}, err)
		}
		row, err := s.DB.SaveSubtitle(ctx, store.SaveSubtitleParams{ID: uuid.NewString(), FileID: in.Params.ID, UserID: route.User(ctx).ID, Name: name, Cues: data})
		return out(SubtitleDTO{ID: row.ID, Name: row.Name, CueCount: int(row.CueCount), Uploaded: true}, err)
	})
	route.Get(s.Routes, op("/files/:id/subtitles/:subtitleId", "getSubtitle", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, SubtitleParams]) (route.Output[SubtitleDocument], error) {
		v := SubtitleDocument{}
		if err := s.subtitleAccess(ctx, in.Params.ID); err != nil {
			return out(v, err)
		}
		f, err := s.DB.GetFile(ctx, in.Params.ID)
		if err != nil {
			return out(v, err)
		}
		var probe media.Probe
		_ = json.Unmarshal(f.Probe, &probe)
		for _, track := range probe.Streams {
			if track.SubtitleID == in.Params.SubtitleID {
				path, err := media.Within(filepath.Join(s.Config.CacheRoot, "subtitles"), filepath.Join(s.Config.CacheRoot, "subtitles", track.SubtitleID+".json"))
				if err != nil {
					return out(v, route.Fail(404, "Subtitle unavailable"))
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return out(v, err)
				}
				err = json.Unmarshal(data, &v.Cues)
				return out(v, err)
			}
		}
		row, err := s.DB.GetSubtitle(ctx, store.GetSubtitleParams{ID: in.Params.SubtitleID, FileID: in.Params.ID, UserID: route.User(ctx).ID})
		if err != nil {
			return out(v, err)
		}
		err = json.Unmarshal(row.Cues, &v.Cues)
		return out(v, err)
	})
}

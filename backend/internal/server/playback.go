package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"jfe/backend/internal/settings"
	"jfe/backend/internal/store"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) playbackDTO(ctx context.Context, p store.PlaybackSession) (PlaybackDTO, error) {
	f, err := s.DB.GetFile(ctx, p.FileID)
	if err != nil {
		return PlaybackDTO{}, err
	}
	file := "index.m3u8"
	var decision PlaybackDecisionDTO
	_ = json.Unmarshal(p.Decision, &decision)
	protocol := "hls"
	if p.Method == "remux" && decision.Container == "mp4" {
		file = "stream.mp4"
		protocol = "mp4"
	}
	if p.Method == "direct" {
		protocol = "file"
	}
	if p.Method == "direct" {
		file = "original"
	}
	dto := PlaybackDTO{Protocol: protocol, ID: p.ID, Method: p.Method, State: p.State, URL: "/api/v1/playback/" + p.ID + "/stream/" + file, Position: p.StartPosition, Duration: f.Duration}
	if decision.Mode != "" {
		dto.Decision = &decision
	}
	if p.State == "ready" {
		var info media.StreamInfo
		if p.Method == "direct" {
			var probe media.Probe
			if err = json.Unmarshal(f.Probe, &probe); err != nil {
				return dto, err
			}
			info = media.SourceStreamInfo(probe, f.Size, f.Duration)
		} else {
			data, err := os.ReadFile(filepath.Join(s.Config.CacheRoot, "playback", p.ID, "stream-info.json"))
			// Sessions created by an older worker have no output measurements.
			if errors.Is(err, os.ErrNotExist) {
				return dto, nil
			}
			if err != nil {
				return dto, err
			}
			if err = json.Unmarshal(data, &info); err != nil {
				return dto, err
			}
		}
		dto.Stream = &PlaybackStreamDTO{ToneMapped: info.ToneMapped, VideoTranscoded: info.VideoTranscoded, VideoCodec: info.VideoCodec, AudioCodec: info.AudioCodec, VideoBitrate: info.VideoBitrate, AudioBitrate: info.AudioBitrate, TotalBitrate: info.TotalBitrate, BitrateSource: info.BitrateSource}
	}
	return dto, nil
}
func (s *Server) startPlayback(ctx context.Context, b PlaybackRequest) (PlaybackDTO, error) {
	f, e := s.DB.GetFile(ctx, b.FileID)
	if e != nil {
		return PlaybackDTO{}, e
	}
	if _, e = s.authorizedItem(ctx, f.ItemID); e != nil {
		return PlaybackDTO{}, e
	}
	if !f.Available {
		return PlaybackDTO{}, route.Fail(409, "Media unavailable")
	}
	if _, e = audio.Within(s.Config.MediaRoot, s.Config.ImportRoot, f.Path); e != nil {
		return PlaybackDTO{}, route.Fail(404, "Media unavailable")
	}
	var probe media.Probe
	if e = json.Unmarshal(f.Probe, &probe); e != nil {
		return PlaybackDTO{}, e
	}
	data, e := s.DB.GetSetting(ctx, "encoding")
	if e != nil {
		return PlaybackDTO{}, e
	}
	cfg, e := media.ParseEncoding(data)
	if e != nil {
		return PlaybackDTO{}, e
	}
	// Manual profiles use server policy. Automatic mode may lower the bitrate
	// further to fit measured network throughput, within the same server ceiling.
	if b.MaxHeight > 0 {
		limit := 0
		if b.AutoQuality {
			limit = b.MaxBitrate
		}
		b.MaxBitrate = cfg.VideoBitrate(b.MaxHeight, limit)
	}
	decision, e := playbackDecision(probe, b, f.Size, f.Duration)
	if e != nil {
		return PlaybackDTO{}, e
	}
	method := string(decision.Mode)
	if decision.Mode == PlaybackModeDirectPlay {
		method = "direct"
	}
	// Preserve the separate legacy audio conversion method.
	if b.Capabilities == nil {
		method, e = playbackMethod(probe, b, f.Size, f.Duration)
	}
	if e != nil {
		return PlaybackDTO{}, e
	}
	state := "preparing"
	if method == "direct" {
		state = "ready"
	}
	if method == "transcode" {
		if cfg.Mode != "nvidia" {
			return PlaybackDTO{}, route.Fail(409, "Video transcoding is disabled")
		}
	}
	if decision.SubtitleID != "" {
		b.SubtitleIndex = -1
	}
	if b.Position > f.Duration {
		b.Position = 0
	}
	id, t := uuid.NewString(), token()
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return PlaybackDTO{}, e
	}
	defer tx.Rollback(ctx)
	q := s.DB.WithTx(tx)
	decisionJSON, _ := json.Marshal(decision)
	e = q.StartPlayback(ctx, store.StartPlaybackParams{Decision: decisionJSON, ID: id, UserID: route.User(ctx).ID, ItemID: f.ItemID, FileID: f.ID, TokenHash: hash(t), Method: method, State: state, StartPosition: b.Position, ExpiresAt: time.Now().Add(6 * time.Hour)})
	if e != nil {
		return PlaybackDTO{}, e
	}
	if method != "direct" {
		payload, _ := json.Marshal(struct {
			PlaybackRequest
			Decision PlaybackDecisionDTO `json:"decision"`
		}{b, decision})
		_, e = q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "transcoder", Kind: "playback", ResourceID: id, Payload: payload})
		if e != nil {
			return PlaybackDTO{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return PlaybackDTO{}, e
	}
	p, e := s.DB.GetPlayback(ctx, id)
	if e != nil {
		return PlaybackDTO{}, e
	}
	v, e := s.playbackDTO(ctx, p)
	v.StreamToken = t
	return v, e
}
func (s *Server) ownPlayback(ctx context.Context, id string) (store.PlaybackSession, error) {
	p, e := s.DB.GetPlayback(ctx, id)
	if e == nil && p.UserID != route.User(ctx).ID {
		e = route.Fail(403, "Playback access denied")
	}
	return p, e
}
func (s *Server) playbackRoutes() {
	r := s.Routes
	route.Post(r, op("/playback", "startPlayback", "user"), func(ctx context.Context, in route.Input[PlaybackRequest, route.Empty, route.Empty]) (route.Output[PlaybackDTO], error) {
		v, e := s.startPlayback(ctx, in.Body)
		return out(v, e)
	})
	route.Get(r, op("/playback/:id", "getPlayback", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[PlaybackDTO], error) {
		p, e := s.ownPlayback(ctx, in.Params.ID)
		if e != nil {
			return out(PlaybackDTO{}, e)
		}
		v, e := s.playbackDTO(ctx, p)
		return out(v, e)
	})
	route.Post(r, op("/playback/:id/progress", "playbackProgress", "user"), func(ctx context.Context, in route.Input[ProgressRequest, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		p, e := s.ownPlayback(ctx, in.Params.ID)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		f, e := s.DB.GetFile(ctx, p.FileID)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		if in.Body.Position > f.Duration+5 {
			return out(HealthDTO{}, route.Fail(422, "Position exceeds duration"))
		}
		general, e := settings.Load(ctx, s.DB)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		defer tx.Rollback(ctx)
		q := s.DB.WithTx(tx)
		p, e = q.PlaybackProgress(ctx, store.PlaybackProgressParams{ID: p.ID, UserID: p.UserID, Position: in.Body.Position, Sequence: in.Body.Sequence})
		if errors.Is(e, pgx.ErrNoRows) {
			return out(HealthDTO{"ignored"}, nil)
		}
		if e != nil {
			return out(HealthDTO{}, e)
		}
		e = q.SaveProgress(ctx, store.SaveProgressParams{UserID: p.UserID, ItemID: p.ItemID, Position: p.Position, Watched: f.Duration > 0 && p.Position >= f.Duration*float64(general.WatchedPercent)/100})
		if e == nil {
			e = tx.Commit(ctx)
		}
		return out(HealthDTO{"ok"}, e)
	})
	route.Delete(r, op("/playback/:id", "stopPlayback", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		p, e := s.ownPlayback(ctx, in.Params.ID)
		if e == nil {
			e = s.DB.StopPlayback(ctx, store.StopPlaybackParams{ID: p.ID, UserID: p.UserID})
		}
		if e == nil {
			e = s.DB.CancelPlaybackJobs(ctx, p.ID)
		}
		return out(HealthDTO{"ok"}, e)
	})
	streamOp := op("/playback/:id/stream/:file", "streamPlayback", "")
	streamOp.Stream = "application/octet-stream"
	route.Get(r, streamOp, func(ctx context.Context, in route.Input[route.Empty, TokenQuery, StreamParams]) (route.Output[StreamDTO], error) {
		p, e := s.DB.GetPlayback(ctx, in.Params.ID)
		if e != nil {
			return out(StreamDTO{}, e)
		}
		if subtle.ConstantTimeCompare([]byte(hash(in.Query.Token)), []byte(p.TokenHash)) != 1 || p.ExpiresAt.Before(time.Now()) || p.State != "ready" {
			return out(StreamDTO{}, route.Fail(403, "Stream unavailable"))
		}
		u, e := s.DB.GetUser(ctx, p.UserID)
		if e != nil || u.Disabled {
			return out(StreamDTO{}, route.Fail(403, "Stream access denied"))
		}
		ctx = route.WithUser(ctx, route.Principal{ID: u.ID, Role: u.Role})
		if _, e = s.authorizedItem(ctx, p.ItemID); e != nil {
			return out(StreamDTO{}, e)
		}
		var path string
		if p.Method == "direct" {
			if in.Params.File != "original" {
				return out(StreamDTO{}, route.Fail(404, "Not found"))
			}
			f, e := s.DB.GetFile(ctx, p.FileID)
			if e != nil {
				return out(StreamDTO{}, e)
			}
			path, e = audio.Within(s.Config.MediaRoot, s.Config.ImportRoot, f.Path)
			if e != nil {
				return out(StreamDTO{}, route.Fail(404, "Media unavailable"))
			}
		} else {
			var decision PlaybackDecisionDTO
			_ = json.Unmarshal(p.Decision, &decision)
			if p.Method == "remux" && decision.Container == "mp4" && in.Params.File != "stream.mp4" {
				return out(StreamDTO{}, route.Fail(404, "Not found"))
			}
			root := filepath.Join(s.Config.CacheRoot, "playback", p.ID)
			path, e = media.Within(root, filepath.Join(root, in.Params.File))
			if e != nil {
				return out(StreamDTO{}, route.Fail(404, "Segment not ready"))
			}
		}
		return route.Output[StreamDTO]{Send: func(c fiber.Ctx) error {
			c.Set("Cache-Control", "private, no-store")
			if strings.HasSuffix(path, "stream.mp4") {
				c.Set("Content-Type", "video/mp4")
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), "complete")); err == nil {
					c.Set("X-Playback-Complete", "true")
				}
				return sendRemuxRange(c, path)
			}
			if strings.HasSuffix(path, ".m3u8") {
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				lines := strings.Split(string(data), "\n")
				for i, line := range lines {
					if line != "" && !strings.HasPrefix(line, "#") {
						lines[i] = line + "?token=" + url.QueryEscape(in.Query.Token)
					}
				}
				c.Set("Content-Type", "application/vnd.apple.mpegurl")
				return c.SendString(strings.Join(lines, "\n"))
			}
			return c.SendFile(path, fiber.SendFile{ByteRange: true})
		}}, nil
	})
	imageOp := op("/items/:id/image", "getItemImage", "")
	castImageOp := op("/items/:id/cast/:personId/image", "getCastImage", "")
	castImageOp.Stream = "image/jpeg"
	route.Get(r, castImageOp, func(ctx context.Context, in route.Input[route.Empty, TokenQuery, CastImageParams]) (route.Output[StreamDTO], error) {
		p, e := s.authenticate(ctx, in.Query.Token)
		if e != nil {
			return out(StreamDTO{}, e)
		}
		i, e := s.authorizedItem(route.WithUser(ctx, p), in.Params.ID)
		if e != nil {
			return out(StreamDTO{}, e)
		}
		var cast []media.CastMember
		if e = json.Unmarshal(i.CastMembers, &cast); e != nil {
			return out(StreamDTO{}, e)
		}
		for _, person := range cast {
			if person.ID != in.Params.PersonID || person.Image == "" {
				continue
			}
			path, err := media.Within(filepath.Join(s.Config.CacheRoot, "artwork"), filepath.Join(s.Config.CacheRoot, "artwork", person.Image))
			if err != nil {
				return out(StreamDTO{}, route.Fail(404, "No image"))
			}
			return route.Output[StreamDTO]{Send: func(c fiber.Ctx) error {
				c.Set("Cache-Control", "private, max-age=300")
				return c.SendFile(path)
			}}, nil
		}
		return out(StreamDTO{}, route.Fail(404, "No image"))
	})
	imageOp.Stream = "image/jpeg"
	route.Get(r, imageOp, func(ctx context.Context, in route.Input[route.Empty, TokenQuery, IDParams]) (route.Output[StreamDTO], error) {
		p, e := s.authenticate(ctx, in.Query.Token)
		if e != nil {
			return out(StreamDTO{}, e)
		}
		ctx = route.WithUser(ctx, p)
		i, e := s.authorizedItem(ctx, in.Params.ID)
		if e != nil {
			return out(StreamDTO{}, e)
		}
		if i.Poster == "" {
			return out(StreamDTO{}, route.Fail(404, "No image"))
		}
		path, e := media.Within(s.Config.CacheRoot, filepath.Join(s.Config.CacheRoot, "artwork", i.Poster))
		if e != nil {
			return out(StreamDTO{}, route.Fail(404, "No image"))
		}
		return route.Output[StreamDTO]{Send: func(c fiber.Ctx) error { c.Set("Cache-Control", "private, max-age=300"); return c.SendFile(path) }}, nil
	})
}

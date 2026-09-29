package server

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	appsettings "jfe/backend/internal/settings"
	"jfe/backend/internal/store"
	"strings"
)

func (s *Server) register() {
	r := s.Routes
	route.Get(r, op("/system", "getSystem", ""), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[SystemDTO], error) {
		if s.DB == nil {
			return out(SystemDTO{}, route.Fail(503, "Database unavailable"))
		}
		n, err := s.DB.CountUsers(ctx)
		if err != nil {
			return route.Output[SystemDTO]{}, fmt.Errorf("get system: count users: %w", err)
		}
		settings, err := s.DB.GetSetting(ctx, "encoding")
		if err != nil {
			return route.Output[SystemDTO]{}, fmt.Errorf("get system: read encoding settings: %w", err)
		}
		encoding, err := media.ParseEncoding(settings)
		if err != nil {
			return route.Output[SystemDTO]{}, fmt.Errorf("get system: parse encoding settings: %w", err)
		}
		general, err := appsettings.Load(ctx, s.DB)
		return out(SystemDTO{Name: general.ServerName, Version: "0.1.0", SetupRequired: n == 0, Capabilities: Capabilities{Music: true, Audio: true, MusicBrainz: true, YouTube: s.youtubeAvailable(ctx), Movies: true, Series: true, Metadata: true, Playback: true, GPU: true, Transcoding: encoding.Mode == "nvidia"}}, err)
	})
	route.Get(r, op("/health", "getHealth", ""), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[HealthDTO], error) {
		if s.Pool == nil || s.Pool.Ping(ctx) != nil {
			return out(HealthDTO{}, route.Fail(503, "Database unavailable"))
		}
		return out(HealthDTO{"ok"}, nil)
	})
	route.Post(r, op("/auth/setup", "setup", ""), func(ctx context.Context, in route.Input[SetupRequest, route.Empty, route.Empty]) (route.Output[LoginDTO], error) {
		v, e := s.setup(ctx, in.Body)
		return out(v, e)
	})
	route.Post(r, op("/auth/login", "login", ""), func(ctx context.Context, in route.Input[LoginRequest, route.Empty, route.Empty]) (route.Output[LoginDTO], error) {
		u, e := s.DB.UserByName(ctx, strings.TrimSpace(in.Body.Username))
		if e != nil || u.Disabled || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Body.Password)) != nil {
			return out(LoginDTO{}, route.Fail(401, "Invalid username or password"))
		}
		v, e := s.login(ctx, u)
		return out(v, e)
	})
	route.Post(r, op("/auth/logout", "logout", "user"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[HealthDTO], error) {
		return out(HealthDTO{"ok"}, s.DB.RevokeUserSessions(ctx, route.User(ctx).ID))
	})
	route.Get(r, op("/users/me", "getMe", "user"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[UserDTO], error) {
		u, e := s.DB.GetUser(ctx, route.User(ctx).ID)
		return out(s.userDTO(ctx, u), e)
	})
	route.Post(r, op("/users/me/password", "changePassword", "user"), func(ctx context.Context, in route.Input[PasswordRequest, route.Empty, route.Empty]) (route.Output[HealthDTO], error) {
		u, e := s.DB.GetUser(ctx, route.User(ctx).ID)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Body.CurrentPassword)) != nil {
			return out(HealthDTO{}, route.Fail(403, "Current password incorrect"))
		}
		pw, e := bcrypt.GenerateFromPassword([]byte(in.Body.NewPassword), bcrypt.DefaultCost)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		e = s.DB.UpdatePassword(ctx, store.UpdatePasswordParams{ID: u.ID, PasswordHash: string(pw)})
		if e == nil {
			e = s.DB.RevokeUserSessions(ctx, u.ID)
		}
		return out(HealthDTO{"ok"}, e)
	})
	route.Get(r, op("/admin/users", "listUsers", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[UsersDTO], error) {
		us, e := s.DB.ListUsers(ctx)
		v := UsersDTO{Items: []UserDTO{}}
		for _, u := range us {
			v.Items = append(v.Items, s.userDTO(ctx, u))
		}
		return out(v, e)
	})
	route.Post(r, op("/admin/users", "createUser", "admin"), func(ctx context.Context, in route.Input[UserRequest, route.Empty, route.Empty]) (route.Output[UserDTO], error) {
		v, e := s.saveUser(ctx, "", in.Body)
		return out(v, e)
	})
	route.Put(r, op("/admin/users/:id", "updateUser", "admin"), func(ctx context.Context, in route.Input[UserRequest, route.Empty, IDParams]) (route.Output[UserDTO], error) {
		v, e := s.saveUser(ctx, in.Params.ID, in.Body)
		return out(v, e)
	})
	route.Delete(r, op("/admin/users/:id", "deleteUser", "admin"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		if in.Params.ID == route.User(ctx).ID {
			return out(HealthDTO{}, route.Fail(409, "Cannot delete yourself"))
		}
		return out(HealthDTO{"ok"}, s.DB.DeleteUser(ctx, in.Params.ID))
	})
	route.Get(r, op("/libraries", "listLibraries", "user"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[LibrariesDTO], error) {
		p := route.User(ctx)
		libs, e := s.DB.ListLibraries(ctx, store.ListLibrariesParams{IsAdmin: p.Role == "admin", UserID: p.ID})
		v := LibrariesDTO{Items: []LibraryDTO{}}
		for _, l := range libs {
			d, err := s.libraryDTO(ctx, l)
			if err != nil {
				return out(v, err)
			}
			if p.Role != "admin" {
				d.Paths = []string{}
			}
			v.Items = append(v.Items, d)
		}
		return out(v, e)
	})
	route.Post(r, op("/libraries", "createLibrary", "admin"), func(ctx context.Context, in route.Input[LibraryRequest, route.Empty, route.Empty]) (route.Output[LibraryDTO], error) {
		v, e := s.saveLibrary(ctx, "", in.Body)
		return out(v, e)
	})
	route.Put(r, op("/libraries/:id", "updateLibrary", "admin"), func(ctx context.Context, in route.Input[LibraryRequest, route.Empty, IDParams]) (route.Output[LibraryDTO], error) {
		v, e := s.saveLibrary(ctx, in.Params.ID, in.Body)
		return out(v, e)
	})
	route.Delete(r, op("/libraries/:id", "deleteLibrary", "admin"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		return out(HealthDTO{"ok"}, s.deleteLibrary(ctx, in.Params.ID))
	})
	route.Post(r, op("/libraries/:id/scan", "scanLibrary", "admin"), func(ctx context.Context, in route.Input[route.Empty, ScanQuery, IDParams]) (route.Output[JobDTO], error) {
		_, e := s.DB.GetLibrary(ctx, in.Params.ID)
		if e != nil {
			return out(JobDTO{}, e)
		}
		v, e := s.enqueue(ctx, "scanner", "scan", in.Params.ID, in.Query)
		return out(v, e)
	})
	route.Get(r, op("/items", "listItems", "user"), func(ctx context.Context, in route.Input[route.Empty, CatalogQuery, route.Empty]) (route.Output[ItemsDTO], error) {
		v, e := s.catalog(ctx, in.Query)
		return out(v, e)
	})
	route.Get(r, op("/items/:id", "getItem", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[DetailDTO], error) {
		v, e := s.detail(ctx, in.Params.ID)
		return out(v, e)
	})
	route.Put(r, op("/items/:id/state", "setItemState", "user"), func(ctx context.Context, in route.Input[StateRequest, route.Empty, IDParams]) (route.Output[ItemDTO], error) {
		v, e := s.authorizedItem(ctx, in.Params.ID)
		if e != nil {
			return out(ItemDTO{}, e)
		}
		_, e = s.DB.SetState(ctx, store.SetStateParams{UserID: route.User(ctx).ID, ItemID: v.ID, Favorite: in.Body.Favorite, Watched: in.Body.Watched})
		return out(s.itemDTO(ctx, v), e)
	})
	route.Put(r, op("/items/:id/metadata", "updateMetadata", "admin"), func(ctx context.Context, in route.Input[MetadataRequest, route.Empty, IDParams]) (route.Output[ItemDTO], error) {
		v, e := s.DB.GetItem(ctx, in.Params.ID)
		if e != nil {
			return out(ItemDTO{}, e)
		}
		b := in.Body
		v, e = s.DB.SaveMetadata(ctx, store.SaveMetadataParams{CastMembers: v.CastMembers, ID: v.ID, Title: b.Title, Year: int32(b.Year), Overview: b.Overview, Poster: v.Poster, ProviderID: b.ProviderID, MetadataLocked: b.Locked})
		return out(s.itemDTO(ctx, v), e)
	})
	route.Get(r, op("/metadata/search", "searchMetadata", "admin"), func(ctx context.Context, in route.Input[route.Empty, MetadataSearchQuery, route.Empty]) (route.Output[MetadataMatches], error) {
		v, e := s.searchMetadata(ctx, in.Query)
		return out(v, e)
	})
	route.Post(r, op("/items/:id/identify", "identifyItem", "admin"), func(ctx context.Context, in route.Input[IdentifyRequest, route.Empty, IDParams]) (route.Output[JobDTO], error) {
		_, e := s.DB.GetItem(ctx, in.Params.ID)
		if e != nil {
			return out(JobDTO{}, e)
		}
		v, e := s.enqueue(ctx, "scanner", "metadata", in.Params.ID, in.Body)
		return out(v, e)
	})
	route.Get(r, op("/admin/jobs", "listJobs", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[JobsDTO], error) {
		jobs, e := s.DB.ListJobDetails(ctx)
		v := JobsDTO{Items: []JobDTO{}}
		for _, j := range jobs {
			d := jobDTO(j.Job)
			d.ResourceName, d.ItemID, d.LibraryID = j.ResourceName, j.ItemID, j.LibraryID
			v.Items = append(v.Items, d)
		}
		return out(v, e)
	})
	route.Post(r, op("/admin/jobs/:id/cancel", "cancelJob", "admin"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		return out(HealthDTO{"ok"}, s.DB.CancelJob(ctx, in.Params.ID))
	})
	route.Get(r, op("/admin/workers", "listWorkers", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[WorkersDTO], error) {
		ws, e := s.DB.ListWorkers(ctx)
		v := WorkersDTO{Items: []WorkerDTO{}}
		for _, w := range ws {
			v.Items = append(v.Items, WorkerDTO{w.ID, w.Role, w.HeartbeatAt})
		}
		return out(v, e)
	})
	route.Get(r, op("/admin/encoding", "getEncoding", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[EncodingDTO], error) {
		b, e := s.DB.GetSetting(ctx, "encoding")
		if e != nil {
			return out(EncodingDTO{}, e)
		}
		cfg, e := media.ParseEncoding(b)
		return out(EncodingDTO{Mode: cfg.Mode, CQ: cfg.CQ, Device: cfg.Device, MaxConcurrent: cfg.MaxConcurrent, Preset: cfg.Preset, VideoCodec: cfg.VideoCodec, Bitrate720: cfg.Bitrate720, Bitrate1080: cfg.Bitrate1080, Bitrate2160: cfg.Bitrate2160, AudioCodec: cfg.AudioCodec, AudioBitrate: cfg.AudioBitrate, SubtitleSize: cfg.SubtitleSize, SubtitleOutline: cfg.SubtitleOutline, SubtitleMargin: cfg.SubtitleMargin, SubtitleFont: cfg.SubtitleFont, SubtitleColor: cfg.SubtitleColor, SubtitleBackground: cfg.SubtitleBackground, SubtitleBackgroundOpacity: cfg.SubtitleBackgroundOpacity, SubtitleBorderColor: cfg.SubtitleBorderColor}, e)
	})
	route.Put(r, op("/admin/encoding", "saveEncoding", "admin"), func(ctx context.Context, in route.Input[EncodingDTO, route.Empty, route.Empty]) (route.Output[EncodingDTO], error) {
		b, _ := json.Marshal(in.Body)
		if _, e := media.ParseEncoding(b); e != nil {
			return out(EncodingDTO{}, route.Fail(422, e.Error()))
		}
		return out(in.Body, s.DB.SaveSetting(ctx, store.SaveSettingParams{Key: "encoding", Value: b}))
	})
	s.audioRoutes()
	s.playbackRoutes()
	s.settingsRoutes()
	s.subtitleRoutes()
}

func (s *Server) youtubeAvailable(ctx context.Context) bool {
	if s.Config.ImportRoot == "" {
		return false
	}
	workers, e := s.DB.ListWorkers(ctx)
	if e != nil {
		return false
	}
	for _, w := range workers {
		if w.Role == "downloader" {
			var cap struct {
				YouTube bool `json:"youtube"`
			}
			_ = json.Unmarshal(w.Capabilities, &cap)
			return cap.YouTube
		}
	}
	return false
}

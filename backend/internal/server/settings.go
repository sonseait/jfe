package server

import (
	"context"
	"encoding/json"
	"strings"

	"jfe/backend/internal/route"
	"jfe/backend/internal/settings"
	"jfe/backend/internal/store"
)

func (s *Server) generalSettings(ctx context.Context) (GeneralSettingsDTO, error) {
	cfg, err := settings.Load(ctx, s.DB)
	return GeneralSettingsDTO{ServerName: cfg.ServerName, AutoMetadata: cfg.AutoMetadata, MetadataLanguage: cfg.MetadataLanguage, CastImages: cfg.CastImages, WatchedPercent: cfg.WatchedPercent, TMDBConfigured: s.Config.TMDBToken != ""}, err
}

func (s *Server) settingsRoutes() {
	route.Get(s.Routes, op("/admin/settings", "getGeneralSettings", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[GeneralSettingsDTO], error) {
		v, err := s.generalSettings(ctx)
		return out(v, err)
	})
	route.Patch(s.Routes, op("/admin/settings", "patchGeneralSettings", "admin"), func(ctx context.Context, in route.Input[GeneralSettingsPatch, route.Empty, route.Empty]) (route.Output[GeneralSettingsDTO], error) {
		if in.Body.ServerName != nil {
			name := strings.TrimSpace(*in.Body.ServerName)
			if name == "" {
				return out(GeneralSettingsDTO{}, route.Fail(422, "Server name must not be blank"))
			}
			in.Body.ServerName = &name
		}
		data, err := json.Marshal(in.Body)
		if err == nil {
			err = s.DB.PatchSetting(ctx, store.PatchSettingParams{Key: settings.Key, Value: data})
		}
		if err != nil {
			return out(GeneralSettingsDTO{}, err)
		}
		v, err := s.generalSettings(ctx)
		return out(v, err)
	})
}

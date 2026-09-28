package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const Key = "general"

type General struct {
	ServerName       string `json:"serverName"`
	AutoMetadata     bool   `json:"autoMetadata"`
	MetadataLanguage string `json:"metadataLanguage"`
	CastImages       bool   `json:"castImages"`
	WatchedPercent   int    `json:"watchedPercent"`
}

func Defaults() General {
	return General{ServerName: "JFE", AutoMetadata: true, MetadataLanguage: "en-US", CastImages: true, WatchedPercent: 95}
}

type Reader interface {
	GetSetting(context.Context, string) ([]byte, error)
}

func Load(ctx context.Context, db Reader) (General, error) {
	cfg := Defaults()
	data, err := db.GetSetting(ctx, Key)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid general settings")
	}
	if strings.TrimSpace(cfg.ServerName) == "" || utf8.RuneCountInString(cfg.ServerName) > 80 || (cfg.MetadataLanguage != "en-US" && cfg.MetadataLanguage != "vi-VN") || cfg.WatchedPercent < 50 || cfg.WatchedPercent > 100 {
		return cfg, fmt.Errorf("invalid general settings")
	}
	return cfg, nil
}

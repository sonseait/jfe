package config

import (
	"errors"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	DatabaseURL, Listen, MediaRoot, CacheRoot, TMDBToken string
	LogLevel, LogFormat                                  string
	Concurrency                                          int
	ImportRoot                                           string
	Python                                               string
	SubtitleEditing                                      bool
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AllowEmptyEnv(true)
	v.AutomaticEnv()
	v.SetDefault("DATABASE_URL", "postgres://postgres@localhost:5432/jfe?sslmode=disable")
	v.SetDefault("JFE_LISTEN", ":8090")
	v.SetDefault("JFE_MEDIA_ROOT", "./media")
	v.SetDefault("JFE_CACHE_ROOT", "./cache")
	v.SetDefault("JFE_CONCURRENCY", 1)
	v.SetDefault("JFE_IMPORT_ROOT", "")
	v.SetDefault("JFE_PYTHON", "python3")
	v.SetDefault("JFE_SUBTITLE_EDITING", false)
	v.SetDefault("TMDB_TOKEN", "")
	v.SetDefault("JFE_LOG_LEVEL", "info")
	v.SetDefault("JFE_LOG_FORMAT", "json")
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Config parser errors may include credentials from the offending line.
		return Config{}, errors.New("cannot load .env: check file permissions and dotenv syntax")
	}
	n := v.GetInt("JFE_CONCURRENCY")
	if n < 1 {
		n = 1
	}
	return Config{
		SubtitleEditing: v.GetBool("JFE_SUBTITLE_EDITING"),
		ImportRoot:      v.GetString("JFE_IMPORT_ROOT"), Python: v.GetString("JFE_PYTHON"),
		DatabaseURL: v.GetString("DATABASE_URL"), Listen: v.GetString("JFE_LISTEN"),
		MediaRoot: v.GetString("JFE_MEDIA_ROOT"), CacheRoot: v.GetString("JFE_CACHE_ROOT"),
		TMDBToken: v.GetString("TMDB_TOKEN"), Concurrency: n,
		LogLevel: v.GetString("JFE_LOG_LEVEL"), LogFormat: v.GetString("JFE_LOG_FORMAT"),
	}, nil
}

package runtime

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"jfe/backend/internal/config"
)

func newLogger(output io.Writer, cfg config.Config) (zerolog.Logger, error) {
	levelName := strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if levelName == "" {
		levelName = "info"
	}
	level, err := zerolog.ParseLevel(levelName)
	if err != nil || level == zerolog.NoLevel {
		return zerolog.Logger{}, errors.New("invalid JFE_LOG_LEVEL: use trace, debug, info, warn, error, fatal, panic or disabled")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.LogFormat)) {
	case "", "json":
	case "console":
		output = zerolog.ConsoleWriter{Out: output, TimeFormat: time.RFC3339, NoColor: true}
	default:
		return zerolog.Logger{}, errors.New("invalid JFE_LOG_FORMAT: use json or console")
	}
	return zerolog.New(output).Level(level).With().Timestamp().Logger(), nil
}

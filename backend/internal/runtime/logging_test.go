package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"jfe/backend/internal/config"
)

func TestDefaultLoggerIsJSONAtInfo(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug().Msg("hidden")
	logger.Info().Str("component", "api").Msg("ready")
	var record map[string]any
	if err = json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["level"] != "info" || record["message"] != "ready" || record["component"] != "api" || record["time"] == nil {
		t.Fatalf("unexpected JSON record: %v", record)
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	for _, level := range []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"} {
		t.Run(level, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := newLogger(&output, config.Config{LogLevel: level, LogFormat: "json"})
			if err != nil {
				t.Fatal(err)
			}
			threshold, _ := zerolog.ParseLevel(level)
			for _, event := range []zerolog.Level{zerolog.TraceLevel, zerolog.DebugLevel, zerolog.InfoLevel, zerolog.WarnLevel, zerolog.ErrorLevel} {
				output.Reset()
				logger.WithLevel(event).Msg("event")
				if got, want := output.Len() > 0, event >= threshold; got != want {
					t.Fatalf("event %s visible=%v want=%v", event, got, want)
				}
			}
		})
	}
}

func TestConsoleLoggerAndInvalidSettings(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, config.Config{LogLevel: "DEBUG", LogFormat: "console"})
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug().Msg("ready")
	if json.Valid(output.Bytes()) || !strings.Contains(output.String(), "DBG") || !strings.Contains(output.String(), "ready") {
		t.Fatalf("unexpected console output: %s", output.String())
	}
	for _, cfg := range []config.Config{{LogLevel: "invalid"}, {LogFormat: "invalid"}} {
		if _, err = newLogger(&output, cfg); err == nil {
			t.Fatal("invalid logging configuration accepted")
		}
	}
}

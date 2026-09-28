package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"DATABASE_URL", "JFE_LISTEN", "JFE_MEDIA_ROOT", "JFE_CACHE_ROOT", "JFE_CONCURRENCY", "TMDB_TOKEN"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".env", []byte("DATABASE_URL=postgres://file/test\nJFE_LISTEN=:9000\nJFE_MEDIA_ROOT=\"/media/my movies\"\nJFE_CACHE_ROOT=/tmp/cache\nJFE_CONCURRENCY=3\nTMDB_TOKEN=file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg.DatabaseURL != "postgres://file/test" || cfg.Listen != ":9000" || cfg.MediaRoot != "/media/my movies" || cfg.CacheRoot != "/tmp/cache" || cfg.Concurrency != 3 || cfg.TMDBToken != "file-token" {
		t.Fatal("dotenv configuration was not loaded correctly")
	}
	t.Setenv("DATABASE_URL", "postgres://environment/test")
	t.Setenv("TMDB_TOKEN", "")
	cfg, err = Load()
	if err != nil || cfg.DatabaseURL != "postgres://environment/test" || cfg.TMDBToken != "" {
		t.Fatal("process environment did not take precedence")
	}
}

func TestLoadWithoutDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("JFE_LISTEN", ":9002")
	cfg, err := Load()
	if err != nil || cfg.Listen != ":9002" {
		t.Fatalf("environment-only configuration failed: %v", err)
	}
}

func TestMalformedDotEnvDoesNotLeakValues(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("TMDB_TOKEN=\"private-unclosed-token"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || strings.Contains(err.Error(), "private-unclosed-token") {
		t.Fatal("expected a sanitized dotenv error")
	}
}

func TestLoggingConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"JFE_LOG_LEVEL", "JFE_LOG_FORMAT"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil || cfg.LogLevel != "info" || cfg.LogFormat != "json" {
		t.Fatal("unexpected logging defaults")
	}
	if err = os.WriteFile(".env", []byte("JFE_LOG_LEVEL=debug\nJFE_LOG_FORMAT=console\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || cfg.LogLevel != "debug" || cfg.LogFormat != "console" {
		t.Fatal("logging settings not loaded from .env")
	}
	t.Setenv("JFE_LOG_LEVEL", "warn")
	t.Setenv("JFE_LOG_FORMAT", "json")
	cfg, err = Load()
	if err != nil || cfg.LogLevel != "warn" || cfg.LogFormat != "json" {
		t.Fatal("environment did not override logging settings")
	}
}

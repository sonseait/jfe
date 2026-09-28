package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestYouTubeDiagnostics(t *testing.T) {
	for _, tc := range []struct{ diagnostic, code string }{
		{"ERROR: Sign in to confirm you're not a bot", "youtube_login_required"},
		{"ERROR: Private video", "youtube_unavailable"},
		{"ERROR: HTTP Error 403: Forbidden", "youtube_forbidden"},
		{"ERROR: HTTP Error 429: Too Many Requests", "youtube_rate_limited"},
		{"ERROR: connection timed out", "youtube_timeout"},
		{"ERROR: Temporary failure in name resolution", "youtube_network_failed"},
		{"ERROR: CERTIFICATE_VERIFY_FAILED", "youtube_tls_failed"},
		{"ERROR: ffprobe and ffmpeg not found", "youtube_dependency_missing"},
		{"ERROR: Requested format is not available", "youtube_format_unavailable"},
		{"ERROR: Unable to extract player data", "youtube_extraction_failed"},
		{"ERROR: No space left on device", "audio_storage_full"},
		{"ERROR: Permission denied", "audio_permission_denied"},
		{"ERROR: unexpected provider response", "youtube_request_failed"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := youtubeCommandError(context.Background(), errors.New("exit status 1"), []byte(tc.diagnostic+" https://example.test/audio?signature=SECRET Authorization: Bearer TOKEN"))
			if code := JobErrorCode(fmt.Errorf("download stage: %w", err)); code != tc.code {
				t.Fatalf("code = %q", code)
			}
			if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "TOKEN") || strings.Contains(err.Error(), "https://") {
				t.Fatalf("unsafe diagnostic: %v", err)
			}
		})
	}
	if code := JobErrorCode(youtubeCommandError(context.Background(), exec.ErrNotFound, nil)); code != "youtube_dependency_missing" {
		t.Fatal(code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := youtubeCommandError(ctx, errors.New("killed"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := youtubeCommandError(context.Background(), errors.New("exit status 1"), []byte("Sign in")); !errors.Is(err, ErrYouTubeLogin) {
		t.Fatal(err)
	}
}

func TestYouTubeCommandFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("#!/bin/sh\necho 'ERROR: HTTP Error 403: Forbidden https://example.test/?signature=SECRET' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	_, err := YouTubeCommand(context.Background(), "--version")
	var detail *YouTubeError
	if !errors.As(err, &detail) || detail.ExitCode != 7 || detail.Code != "youtube_forbidden" {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("signed URL escaped provider boundary")
	}
}

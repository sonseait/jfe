package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

var ErrStorageFull = errors.New("audio_storage_full")

type YouTubeError struct {
	Code     string
	Reason   string
	ExitCode int
	cause    error
}

func (e *YouTubeError) Error() string {
	return fmt.Sprintf("%s: %s (yt-dlp exit code %d)", e.Code, e.Reason, e.ExitCode)
}
func (e *YouTubeError) Unwrap() error { return e.cause }

// Only fixed diagnostic categories leave the provider boundary. Remote stderr
// can contain signed URLs, credentials or arbitrary metadata.
func youtubeCommandError(ctx context.Context, cause error, diagnostic []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	e := &YouTubeError{Code: "youtube_request_failed", Reason: "yt-dlp command failed; no recognized diagnostic", ExitCode: -1, cause: ErrYouTubeRequest}
	var exit *exec.ExitError
	if errors.As(cause, &exit) {
		e.ExitCode = exit.ExitCode()
	}
	if errors.Is(cause, exec.ErrNotFound) || errors.Is(cause, os.ErrNotExist) {
		e.Code, e.Reason = "youtube_dependency_missing", "yt-dlp executable not found"
		return e
	}
	if errors.Is(cause, os.ErrPermission) {
		e.Code, e.Reason = "audio_permission_denied", "permission denied starting yt-dlp"
		return e
	}
	s := strings.ToLower(string(diagnostic))
	for _, rule := range []struct {
		code, reason string
		matches      []string
	}{
		{"youtube_login_required", "YouTube requires sign-in, age or bot verification", []string{"sign in", "confirm your age", "confirm you're not a bot"}},
		{"youtube_rate_limited", "YouTube HTTP 429: too many requests", []string{"http error 429", "429 too many requests"}},
		{"youtube_forbidden", "YouTube or media CDN HTTP 403: access forbidden", []string{"http error 403", "403 forbidden"}},
		{"youtube_timeout", "provider connection timed out", []string{"timed out", "timeout"}},
		{"youtube_network_failed", "DNS lookup or network connection failed", []string{"name resolution", "name or service not known", "nodename nor servname", "connection refused", "network is unreachable", "connection reset"}},
		{"youtube_tls_failed", "TLS certificate verification failed", []string{"certificate_verify_failed", "certificate verify failed"}},
		{"youtube_dependency_missing", "required FFmpeg or JavaScript runtime is unavailable", []string{"ffmpeg not found", "ffprobe not found", "ffprobe and ffmpeg not found", "no supported javascript runtime"}},
		{"youtube_format_unavailable", "requested AAC/Opus audio format is unavailable", []string{"requested format is not available"}},
		{"youtube_extraction_failed", "YouTube extraction or JavaScript challenge failed; check yt-dlp/runtime version", []string{"signature solving failed", "n challenge solving failed", "unable to extract", "javascript challenge"}},
		{"audio_storage_full", "download storage has insufficient free space", []string{"no space left on device", "disk quota exceeded"}},
		{"audio_permission_denied", "download storage permission denied", []string{"permission denied"}},
		{"youtube_unavailable", "video is unavailable, private or removed", []string{"video unavailable", "video is unavailable", "private video", "video has been removed", "not available in your country"}},
	} {
		for _, match := range rule.matches {
			if strings.Contains(s, match) {
				e.Code, e.Reason = rule.code, rule.reason
				if e.Code == "youtube_login_required" {
					e.cause = ErrYouTubeLogin
				}
				if e.Code == "youtube_unavailable" {
					e.cause = ErrYouTubeUnavailable
				}
				return e
			}
		}
	}
	return e
}

// Job errors contain a public code, while worker logs retain the contextual error.
func JobErrorCode(err error) string {
	var youtube *YouTubeError
	if errors.As(err, &youtube) {
		return youtube.Code
	}
	for _, known := range []error{ErrYouTubeLogin, ErrYouTubeUnavailable, ErrYouTubeRequest, ErrYouTubePartial, ErrStorageFull} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "youtube_timeout"
	}
	if errors.Is(err, os.ErrPermission) {
		return "audio_permission_denied"
	}
	if errors.Is(err, syscall.ENOSPC) {
		return "audio_storage_full"
	}
	return ""
}

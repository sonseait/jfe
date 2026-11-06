package subtitleprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"jfe/backend/internal/media"
)

var diagnosticURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
var diagnosticBearer = regexp.MustCompile(`(?i)bearer\s+\S+`)
var diagnosticAssignment = regexp.MustCompile(`(?i)(api[-_ ]?key|token|password|authorization|username)\s*[=:]\s*[^\s,;]+`)
var diagnosticOpaque = regexp.MustCompile(`[A-Za-z0-9_+/=.-]{24,}`)

func (c *Client) sanitize(value string) string {
	secrets := append([]string{c.Key, c.Token}, c.redactions...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	value = diagnosticURL.ReplaceAllString(value, "[redacted URL]")
	value = diagnosticBearer.ReplaceAllString(value, "Bearer [redacted]")
	value = diagnosticAssignment.ReplaceAllString(value, "[redacted credential]")
	value = diagnosticOpaque.ReplaceAllStringFunc(value, func(part string) string {
		if _, e := uuid.Parse(part); e == nil {
			return part
		}
		return "[redacted value]"
	})
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(value, ""))
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512]) + "..."
	}
	return value
}

func (c *Client) diagnostic(res *http.Response, method, endpoint, reason string) *Error {
	e := &Error{Code: "provider_error", Method: method, Endpoint: endpoint, Reason: reason, RetryAt: time.Now().Add(5 * time.Minute)}
	// A malformed individual file says nothing about API availability. Keep
	// its diagnostics, but do not block searches or other subtitle downloads.
	if endpoint == "subtitle_file" && reason == "invalid_subtitle_response" {
		e.RetryAt = time.Time{}
	}
	if res != nil {
		e.HTTPStatus = res.StatusCode
		e.ContentType, _, _ = mime.ParseMediaType(res.Header.Get("Content-Type"))
		e.ContentType = c.sanitize(e.ContentType)
		e.RequestID = c.sanitize(res.Header.Get("X-Request-ID"))
		if res.Request != nil {
			e.Host = res.Request.URL.Hostname()
		}
	}
	if e.Host == "" {
		e.Host = "api.opensubtitles.com"
		if strings.Contains(c.BaseURL, "vip-api.opensubtitles.com") {
			e.Host = "vip-api.opensubtitles.com"
		}
	}
	return e
}

func (c *Client) connectionError(err error, method, endpoint, host string) error {
	// Classify the cause without printing net/url errors, which include URLs.
	reason := "connection_failed"
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var op *net.OpError
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.As(err, &dns):
		reason = "dns_lookup_failed"
	case errors.As(err, &cert), errors.As(err, &unknownCA):
		reason = "tls_verification_failed"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "request_timeout"
	case errors.As(err, &netErr) && netErr.Timeout():
		reason = "request_timeout"
	case errors.As(err, &op):
		reason = "network_" + op.Op + "_failed"
	}
	result := c.diagnostic(nil, method, endpoint, reason)
	result.Host = host
	return result
}

func (c *Client) decodingError(err error, res *http.Response, method, endpoint string) error {
	e := c.diagnostic(res, method, endpoint, "invalid_json_response")
	var mismatch *json.UnmarshalTypeError
	var syntax *json.SyntaxError
	switch {
	case errors.As(err, &mismatch):
		e.Reason = "response_type_mismatch"
		e.Detail = c.sanitize(fmt.Sprintf("field %s: expected %s, received %s", mismatch.Field, mismatch.Type, mismatch.Value))
	case errors.As(err, &syntax):
		e.Detail = fmt.Sprintf("invalid JSON at byte %d", syntax.Offset)
	default:
		e.Detail = "response is empty, truncated or not valid JSON"
	}
	return e
}

// subtitleError records structural diagnostics only, never subtitle text or a
// body snippet. Content-Type alone cannot establish the downloaded format.
func (c *Client) subtitleError(err error, res *http.Response, name string, data []byte) error {
	e := c.diagnostic(res, "GET", "subtitle_file", "invalid_subtitle_response")
	e.Detail = c.sanitize("invalid downloaded subtitles: " + err.Error())
	e.Filename = c.sanitize(name)
	e.ResponseBytes = len(data)
	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	prefix := strings.ToLower(text[:min(len(text), 1024)])
	switch {
	case strings.HasPrefix(text, "WEBVTT"):
		e.ResponseFormat = "webvtt"
	case strings.HasPrefix(prefix, "<!doctype html"), strings.HasPrefix(prefix, "<html"):
		e.ResponseFormat = "html"
	case strings.HasPrefix(prefix, "<?xml"):
		e.ResponseFormat = "xml"
	case (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) && json.Valid(data):
		e.ResponseFormat = "json"
	case strings.Contains(prefix, "[script info]") || strings.Contains(prefix, "[events]"):
		e.ResponseFormat = "ass_or_ssa"
	case strings.Contains(text, "-->"):
		e.ResponseFormat = "srt_or_webvtt"
	default:
		e.ResponseFormat = "unknown_text"
	}
	var parse *media.SubtitleParseError
	if errors.As(err, &parse) {
		e.ParserReason = parse.Reason
		e.ParserLine = parse.Line
		e.ParsedCues = parse.ParsedCues
	}
	return e
}

// MarshalZerologObject exposes only bounded and redacted diagnostic fields.
func (e *Error) MarshalZerologObject(event *zerolog.Event) {
	event.Str("name", "opensubtitles").Str("code", e.Code)
	if e.Filename != "" {
		event.Str("filename", e.Filename)
	}
	if e.ResponseFormat != "" {
		event.Str("responseFormat", e.ResponseFormat).Int("responseBytes", e.ResponseBytes)
	}
	if e.ParserReason != "" {
		event.Str("parserReason", e.ParserReason).Int("parsedCues", e.ParsedCues)
	}
	if e.ParserLine > 0 {
		event.Int("parserLine", e.ParserLine)
	}
	if e.URL != "" {
		event.Str("url", e.URL).Bool("urlRedacted", e.URLRedacted)
	}
	if e.TimeoutMs > 0 {
		event.Int64("timeoutMs", e.TimeoutMs)
	}
	if e.DurationMs > 0 {
		event.Int64("durationMs", e.DurationMs)
	}
	if e.Method != "" {
		event.Str("method", e.Method)
	}
	if e.Host != "" {
		event.Str("host", e.Host)
	}
	if e.Endpoint != "" {
		event.Str("endpoint", e.Endpoint)
	}
	if e.HTTPStatus != 0 {
		event.Int("httpStatus", e.HTTPStatus).Str("httpStatusText", http.StatusText(e.HTTPStatus))
	}
	if e.ContentType != "" {
		event.Str("contentType", e.ContentType)
	}
	if e.RequestID != "" {
		event.Str("requestId", e.RequestID)
	}
	if e.WaitStage != "" {
		event.Str("waitStage", e.WaitStage)
	}
	if e.LeaseOwner != "" {
		event.Str("leaseOwner", e.LeaseOwner)
	}
	if !e.LeaseUntil.IsZero() {
		event.Time("leaseUntil", e.LeaseUntil)
	}
	if e.Reason != "" {
		event.Str("reason", e.Reason)
	}
	if e.Detail != "" {
		event.Str("detail", e.Detail)
	}
	if !e.RetryAt.IsZero() {
		event.Time("retryAt", e.RetryAt)
	}
	if !e.ResetAt.IsZero() {
		event.Time("resetAt", e.ResetAt)
	}
	if e.Remaining != nil {
		event.Int("remaining", *e.Remaining)
	}
	if e.Deferred {
		event.Bool("requestSkipped", true)
	}
}

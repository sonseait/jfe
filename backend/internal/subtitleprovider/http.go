package subtitleprovider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	apiTimeout          = 10 * time.Second
	downloadTimeout     = 15 * time.Second
	operationTimeout    = 30 * time.Second
	providerWaitTimeout = 5 * time.Second
)

var providerTransport = &http.Transport{
	Proxy:             http.ProxyFromEnvironment,
	DialContext:       (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2: true,
	MaxIdleConns:      20, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second,
	TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
	ExpectContinueTimeout: time.Second,
}

// LogURL reconstructs only known public API routes and search parameters.
// Download links can contain bearer-equivalent signatures in path or query.
func LogURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !downloadURL(u) {
		return "", true
	}
	base := "https://" + u.Host
	if u.Hostname() != "api.opensubtitles.com" && u.Hostname() != "vip-api.opensubtitles.com" {
		return base + "/[redacted]", true
	}
	switch u.Path {
	case "/api/v1/login", "/api/v1/infos/user", "/api/v1/subtitles", "/api/v1/download":
	default:
		return base + "/[redacted]", true
	}
	allowed := map[string]bool{"languages": true, "order_by": true, "order_direction": true, "query": true, "season_number": true, "episode_number": true, "type": true, "tmdb_id": true, "parent_tmdb_id": true, "year": true, "moviehash": true}
	values := url.Values{}
	redacted := u.Fragment != ""
	for key, items := range u.Query() {
		if !allowed[key] {
			redacted = true
			continue
		}
		for _, value := range items {
			if len(value) > 512 {
				redacted = true
				continue
			}
			values.Add(key, value)
		}
	}
	result := base + u.EscapedPath()
	if query := values.Encode(); query != "" {
		result += "?" + query
	}
	return result, redacted
}

func (c *Client) requestError(err error, req *http.Request, res *http.Response, started time.Time, timeout time.Duration) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var pe *Error
	if !errors.As(err, &pe) {
		pe = c.diagnostic(res, req.Method, "subtitle_file", "invalid_subtitle_response")
		pe.Detail = c.sanitize(err.Error())
	}
	target := req.URL
	if res != nil && res.Request != nil {
		target = res.Request.URL
	}
	// Redact known credentials even if a title/search value happens to contain one.
	copyURL := *target
	query := copyURL.Query()
	for key, items := range query {
		for i, value := range items {
			items[i] = c.sanitize(value)
		}
		query[key] = items
	}
	copyURL.RawQuery = query.Encode()
	pe.URL, pe.URLRedacted = LogURL(copyURL.String())
	if copyURL.RawQuery != target.RawQuery {
		pe.URLRedacted = true
	}
	pe.DurationMs = time.Since(started).Milliseconds()
	pe.TimeoutMs = timeout.Milliseconds()
	if deadline, ok := req.Context().Deadline(); ok {
		effective := deadline.Sub(started).Milliseconds()
		if effective < pe.TimeoutMs {
			pe.TimeoutMs = max(0, effective)
		}
	}
	return pe
}

func providerWaitError(ctx context.Context, stage string, started time.Time) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return &Error{Code: "provider_error", Reason: "provider_wait_timeout", WaitStage: stage, DurationMs: time.Since(started).Milliseconds(), Deferred: true, TimeoutMs: providerWaitTimeout.Milliseconds(), RetryAt: time.Now().Add(5 * time.Second)}
}

func responseTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

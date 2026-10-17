package subtitleprovider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
)

// Error keeps the stable public code separate from sanitized log diagnostics.
// Never attach raw response bodies, credentials or signed URLs.
type Error struct {
	Filename       string
	ResponseFormat string
	ResponseBytes  int
	ParserReason   string
	ParserLine     int
	ParsedCues     int
	Code           string
	RetryAt        time.Time
	Remaining      *int
	ResetAt        time.Time
	Method         string
	Host           string
	Endpoint       string
	HTTPStatus     int
	RequestID      string
	ContentType    string
	WaitStage      string
	Reason         string
	Detail         string
	URL            string
	URLRedacted    bool
	DurationMs     int64
	TimeoutMs      int64
	Deferred       bool
	LeaseOwner     string
	LeaseUntil     time.Time
}

func (e *Error) Error() string {
	message := e.Code
	if e.Method != "" {
		message += ": " + e.Method + " " + e.Endpoint
	}
	if e.HTTPStatus != 0 {
		message += fmt.Sprintf(": HTTP %d %s", e.HTTPStatus, http.StatusText(e.HTTPStatus))
	}
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	return message
}

func (c *Client) responseError(res *http.Response, method, endpoint string) error {
	code := "provider_error"
	wait := time.Now().Add(5 * time.Minute)
	switch res.StatusCode {
	case 401, 403:
		code = "authentication_failed"
		wait = time.Now().Add(time.Hour)
	case 406:
		code = "quota_exhausted"
		wait = time.Now().Add(24 * time.Hour)
	case 429:
		code = "rate_limited"
	}
	if raw := res.Header.Get("Retry-After"); raw != "" {
		if seconds, e := strconv.Atoi(raw); e == nil && seconds >= 0 {
			wait = time.Now().Add(time.Duration(seconds) * time.Second)
		} else if at, e := http.ParseTime(raw); e == nil {
			wait = at
		}
	}
	var body struct {
		Remaining *int   `json:"remaining"`
		Reset     string `json:"reset_time_utc"`
		Message   string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	reset := providerTime(body.Reset)
	if code == "quota_exhausted" && reset.After(time.Now()) {
		wait = reset
	}
	e := c.diagnostic(res, method, endpoint, "http_error")
	e.Code = code
	e.RetryAt = wait
	e.Remaining = body.Remaining
	e.ResetAt = reset
	e.Detail = c.sanitize(body.Message)
	return e
}

func providerTime(raw string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if at, e := time.Parse(layout, raw); e == nil {
			return at
		}
	}
	return time.Time{}
}

func (c *Client) apiBase() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.opensubtitles.com/api/v1"
}

func (c *Client) Login(ctx context.Context, username, password string) error {
	var result struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
		User    struct {
			VIP     bool `json:"vip"`
			Allowed int  `json:"allowed_downloads"`
		} `json:"user"`
	}
	c.Token = ""
	c.BaseURL = ""
	c.redactions = []string{username, password}
	c.accountAt = time.Time{}
	if err := c.request(ctx, "POST", "/login", struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{username, password}, &result); err != nil {
		return err
	}
	if result.Token == "" {
		return errors.New("invalid subtitle login response")
	}
	if result.BaseURL != "" {
		base := result.BaseURL
		if !strings.Contains(base, "://") {
			base = "https://" + base
		}
		u, err := url.Parse(base)
		if err != nil || u.Scheme != "https" || u.User != nil || (u.Host != "api.opensubtitles.com" && u.Host != "vip-api.opensubtitles.com") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/" && u.Path != "/api/v1") {
			return errors.New("invalid subtitle API host")
		}
		c.BaseURL = "https://" + u.Host + "/api/v1"
	}
	c.Token = result.Token
	c.VIP = result.User.VIP
	return nil
}

func (c *Client) Account(ctx context.Context) error {
	var result struct {
		Data struct {
			VIP       bool `json:"vip"`
			Remaining *int `json:"remaining_downloads"`
		} `json:"data"`
	}
	if err := c.request(ctx, "GET", "/infos/user", nil, &result); err != nil {
		return err
	}
	c.VIP = result.Data.VIP
	if result.Data.Remaining != nil {
		c.Remaining = *result.Data.Remaining
	}
	c.accountAt = time.Now()
	return nil
}

var session = struct {
	gate     chan struct{}
	identity [32]byte
	client   Client
}{gate: make(chan struct{}, 1)}

// Do serializes provider operations using a fenced, expiring database lease.
// No PostgreSQL session lock, connection or transaction spans network requests.
// Tokens are cached only in process memory; persisted state contains no secrets.
func Do(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, fn func(context.Context, *Client) error) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			resultErr = &Error{Code: "provider_error", Reason: "operation_timeout", TimeoutMs: operationTimeout.Milliseconds(), RetryAt: time.Now().Add(5 * time.Minute)}
		}
	}()
	if cfg.OpenSubtitlesKey == "" {
		return &Error{Code: "not_configured", RetryAt: time.Now().Add(time.Hour)}
	}
	// Serialize access to the in-memory session without holding a database
	// connection while waiting or making provider HTTP requests.
	waitStarted := time.Now()
	waitCtx, endWait := context.WithTimeout(ctx, providerWaitTimeout)
	defer endWait()
	select {
	case session.gate <- struct{}{}:
	case <-waitCtx.Done():
		return providerWaitError(waitCtx, "local_session", waitStarted)
	}
	defer func() { <-session.gate }()
	q := store.New(pool)
	leaseID := uuid.NewString()
	leaseWaitError := func() error {
		e := providerWaitError(waitCtx, "provider_lease", waitStarted)
		var pe *Error
		if errors.As(e, &pe) {
			lookup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if holder, err := q.GetSubtitleProviderLease(lookup); err == nil {
				pe.LeaseOwner = holder.LeaseID
				pe.LeaseUntil = holder.LeaseUntil
			}
		}
		return e
	}
	// This cleanup also handles an acquisition whose reply was lost. Its owner
	// ID cannot release a replacement owner's lease, and failed cleanup expires.
	leaseAcquired := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		rows, err := q.ReleaseSubtitleProviderLease(cleanup, leaseID)
		if err != nil {
			log.Ctx(ctx).Warn().Str("providerLeaseId", leaseID).Msg("OpenSubtitles lease release failed; lease will expire")
		} else if leaseAcquired && rows != 1 {
			log.Ctx(ctx).Warn().Str("providerLeaseId", leaseID).Msg("OpenSubtitles lease ownership changed before release")
		} else if rows == 1 {
			log.Ctx(ctx).Debug().Str("providerLeaseId", leaseID).Msg("OpenSubtitles provider lease released")
		}
	}()
	var leaseUntil time.Time
	for {
		var err error
		leaseUntil, err = q.ClaimSubtitleProviderLease(waitCtx, leaseID)
		if err == nil {
			leaseAcquired = true
			break
		}
		if waitCtx.Err() != nil {
			return leaseWaitError()
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		select {
		case <-waitCtx.Done():
			return leaseWaitError()
		case <-time.After(250 * time.Millisecond):
		}
	}
	endWait()
	leaseStarted := time.Now()
	log.Ctx(ctx).Debug().Str("providerLeaseId", leaseID).Time("providerLeaseUntil", leaseUntil).Msg("OpenSubtitles provider lease acquired")
	defer func() {
		log.Ctx(ctx).Debug().Str("providerLeaseId", leaseID).Int64("heldMs", time.Since(leaseStarted).Milliseconds()).Msg("OpenSubtitles provider operation finished")
	}()
	state, err := q.GetSubtitleProviderState(ctx)
	if err != nil {
		return err
	}
	if state.RetryAt.After(time.Now()) {
		return &Error{Code: state.Status, RetryAt: state.RetryAt, Reason: "provider_backoff", Deferred: true}
	}
	identity := sha256.Sum256([]byte(cfg.OpenSubtitlesKey + "\x00" + cfg.OpenSubtitlesToken + "\x00" + cfg.OpenSubtitlesUsername + "\x00" + cfg.OpenSubtitlesPassword))
	if session.identity != identity {
		session.identity = identity
		session.client = Client{Key: cfg.OpenSubtitlesKey, Token: cfg.OpenSubtitlesToken, Remaining: -1}
	}
	c := &session.client
	c.Remaining = int(state.Remaining)
	c.ResetAt = state.ResetAt
	c.VIP = state.Vip
	if state.Status == "quota_exhausted" && !state.RetryAt.After(time.Now()) {
		c.Remaining = -1
	}
	save := func(opErr error) error {
		status, retry := "connected", time.Unix(0, 0)
		if opErr != nil {
			status = "provider_error"
			retry = time.Now().Add(5 * time.Minute)
			var pe *Error
			if errors.As(opErr, &pe) {
				status = pe.Code
				retry = pe.RetryAt
			}
			if errors.Is(opErr, context.Canceled) || errors.Is(opErr, context.DeadlineExceeded) {
				status = state.Status
				retry = state.RetryAt
			}
		}
		if retry.IsZero() {
			retry = time.Unix(0, 0)
		}
		// A file can fail validation after the download consumed the last quota
		// slot. Preserve quota enforcement even when that file has no backoff.
		if c.Remaining == 0 && (opErr == nil || !retry.After(time.Now())) {
			retry = c.ResetAt
			if !retry.After(time.Now()) {
				retry = time.Now().Add(24 * time.Hour)
			}
			status = "quota_exhausted"
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rows, e := q.SaveSubtitleProviderState(persistCtx, store.SaveSubtitleProviderStateParams{LeaseID: leaseID, Status: status, Vip: c.VIP, Remaining: int32(c.Remaining), ResetAt: c.ResetAt, RetryAt: retry})
		if e == nil && rows != 1 {
			e = &Error{Code: "provider_error", Reason: "provider_lease_lost", LeaseOwner: leaseID, LeaseUntil: leaseUntil, RetryAt: time.Now().Add(5 * time.Second)}
		}
		if opErr != nil {
			if e != nil {
				log.Ctx(ctx).Warn().Str("providerLeaseId", leaseID).Msg("OpenSubtitles provider state could not be saved")
			}
			return opErr
		}
		return e
	}
	if c.Token == "" && cfg.OpenSubtitlesToken == "" && cfg.OpenSubtitlesUsername != "" && cfg.OpenSubtitlesPassword != "" {
		if err = c.Login(ctx, cfg.OpenSubtitlesUsername, cfg.OpenSubtitlesPassword); err != nil {
			return save(err)
		}
	}
	// Refresh account details sparingly; anonymous API-key searches remain supported.
	if c.Token != "" && (c.accountAt.Before(time.Now().Add(-time.Hour)) || state.Status != "connected") {
		err = c.Account(ctx)
		var pe *Error
		if errors.As(err, &pe) && pe.Code == "authentication_failed" && cfg.OpenSubtitlesToken == "" && cfg.OpenSubtitlesUsername != "" {
			err = c.Login(ctx, cfg.OpenSubtitlesUsername, cfg.OpenSubtitlesPassword)
			if err == nil {
				err = c.Account(ctx)
			}
		}
		if err != nil {
			return save(err)
		}
	}
	if c.Remaining == 0 {
		retry := c.ResetAt
		if !retry.After(time.Now()) {
			retry = time.Now().Add(24 * time.Hour)
		}
		return save(&Error{Code: "quota_exhausted", RetryAt: retry})
	}
	err = fn(ctx, c)
	var pe *Error
	if errors.As(err, &pe) && pe.Code == "authentication_failed" && cfg.OpenSubtitlesToken == "" && cfg.OpenSubtitlesUsername != "" {
		err = c.Login(ctx, cfg.OpenSubtitlesUsername, cfg.OpenSubtitlesPassword)
		if err == nil {
			err = fn(ctx, c)
		}
	}
	return save(err)
}

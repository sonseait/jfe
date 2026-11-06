package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var ErrTMDBNotConfigured = errors.New("TMDB_TOKEN not configured")

type ProviderError struct{ Status int }

func (e *ProviderError) Error() string { return fmt.Sprintf("metadata provider returned %d", e.Status) }

func TMDB(ctx context.Context, token, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.themoviedb.org/3"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return &ProviderError{Status: res.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(target)
}

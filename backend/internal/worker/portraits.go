package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"time"
)

var profilePath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(?:jpg|png)$`)

func (w *Worker) castPortrait(ctx context.Context, id, profile string) (string, error) {
	if !profilePath.MatchString(profile) {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://image.tmdb.org/t/p/w185"+profile, nil)
	if err != nil {
		return "", err
	}
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cast image provider returned %d", res.StatusCode)
	}
	if err = w.savePortrait(id, res.Body); err != nil {
		return "", err
	}
	return "cast-" + id + ".jpg", nil
}

func (w *Worker) savePortrait(id string, input io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("cast image too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return fmt.Errorf("invalid cast image dimensions")
	}
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	return w.saveArtwork("cast-"+id, &encoded)
}

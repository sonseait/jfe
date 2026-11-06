package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"time"

	_ "golang.org/x/image/webp"
)

var profilePath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(?:jpe?g|png|webp|gif)$`)

func (w *Worker) castPortrait(ctx context.Context, id, profile string) (string, error) {
	found, err := w.downloadTMDBArtwork(ctx, "cast-"+id, profile, "w185")
	if err != nil || !found {
		return "", err
	}
	return "cast-" + id + ".jpg", nil
}

func (w *Worker) downloadTMDBArtwork(ctx context.Context, id, profile, size string) (bool, error) {
	if !profilePath.MatchString(profile) {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://image.tmdb.org/t/p/"+size+profile, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "image/jpeg, image/png, image/webp, image/gif")
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("artwork provider returned %d", res.StatusCode)
	}
	if err = w.saveJPEGArtwork(id, res.Body); err != nil {
		return false, err
	}
	return true, nil
}

func (w *Worker) savePortrait(id string, input io.Reader) error {
	return w.saveJPEGArtwork("cast-"+id, input)
}

// Decode by content rather than suffix, then publish a verified JPEG atomically.
func (w *Worker) saveJPEGArtwork(id string, input io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("artwork image too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode artwork (%s): %w", http.DetectContentType(data), err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return fmt.Errorf("invalid artwork image dimensions")
	}
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	return w.saveArtwork(id, &encoded)
}

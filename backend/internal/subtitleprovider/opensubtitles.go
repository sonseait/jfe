package subtitleprovider

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/unicode"
	"jfe/backend/internal/media"
)

const textLimit = 512 * 1024

type Client struct{ Key, Token string }
type Candidate struct {
	FileID                  int
	Name, Language, Release string
	Downloads               int
	HearingImpaired         bool
}

// Provider errors deliberately omit response bodies, signed URLs and credentials.
func (c Client) request(ctx context.Context, method, path string, body, target any) error {
	if c.Key == "" {
		return errors.New("OpenSubtitles is not configured")
	}
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.opensubtitles.com/api/v1"+path, input)
	if err != nil {
		return errors.New("invalid subtitle provider request")
	}
	req.Header.Set("Api-Key", c.Key)
	req.Header.Set("User-Agent", "JFE v0.1.0")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("subtitle provider connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("subtitle provider returned %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(target); err != nil {
		return errors.New("invalid subtitle provider response")
	}
	return nil
}

func (c Client) Search(ctx context.Context, query url.Values) ([]Candidate, error) {
	var result struct {
		Data []struct {
			Attributes struct {
				Language        string `json:"language"`
				Release         string `json:"release"`
				Downloads       int    `json:"download_count"`
				HearingImpaired bool   `json:"hearing_impaired"`
				Files           []struct {
					ID   int    `json:"file_id"`
					Name string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.request(ctx, "GET", "/subtitles?"+query.Encode(), nil, &result); err != nil {
		return nil, err
	}
	items := []Candidate{}
	for _, row := range result.Data {
		for _, f := range row.Attributes.Files {
			if f.ID > 0 {
				items = append(items, Candidate{f.ID, f.Name, row.Attributes.Language, row.Attributes.Release, row.Attributes.Downloads, row.Attributes.HearingImpaired})
			}
			if len(items) >= 100 {
				return items, nil
			}
		}
	}
	return items, nil
}

func downloadURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") &&
		(host == "opensubtitles.com" || strings.HasSuffix(host, ".opensubtitles.com") || host == "opensubtitles.org" || strings.HasSuffix(host, ".opensubtitles.org"))
}

func (c Client) Download(ctx context.Context, fileID int) (string, []media.Cue, error) {
	var result struct {
		Link string `json:"link"`
		Name string `json:"file_name"`
	}
	if err := c.request(ctx, "POST", "/download", struct {
		ID     int    `json:"file_id"`
		Format string `json:"sub_format"`
	}{fileID, "srt"}, &result); err != nil {
		return "", nil, err
	}
	u, err := url.Parse(result.Link)
	if err != nil || !downloadURL(u) {
		return "", nil, errors.New("invalid subtitle download host")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", nil, errors.New("invalid subtitle download")
	}
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !downloadURL(req.URL) {
			return errors.New("invalid subtitle redirect")
		}
		return nil
	}}
	res, err := client.Do(req)
	if err != nil {
		return "", nil, errors.New("subtitle download connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("subtitle download returned %d", res.StatusCode)
	}
	data, err := boundedRead(res.Body, 4<<20)
	if err != nil {
		return "", nil, err
	}
	name := filepath.Base(strings.ReplaceAll(result.Name, "\\", "/"))
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{'P', 'K', 3, 4}) {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(archive.File) > 20 {
			return "", nil, errors.New("invalid subtitle archive")
		}
		found := false
		for _, entry := range archive.File {
			ext := strings.ToLower(filepath.Ext(entry.Name))
			if entry.FileInfo().IsDir() || (ext != ".srt" && ext != ".vtt") {
				continue
			}
			if entry.UncompressedSize64 > textLimit {
				return "", nil, errors.New("subtitle exceeds 512 KiB")
			}
			f, err := entry.Open()
			if err != nil {
				return "", nil, errors.New("invalid subtitle archive entry")
			}
			data, err = boundedRead(f, textLimit)
			f.Close()
			if err != nil {
				return "", nil, err
			}
			name, found = filepath.Base(strings.ReplaceAll(entry.Name, "\\", "/")), true
			break
		}
		if !found {
			return "", nil, errors.New("archive has no SRT or WebVTT subtitle")
		}
	} else if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		f, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", nil, errors.New("invalid subtitle gzip")
		}
		data, err = boundedRead(f, textLimit)
		f.Close()
		if err != nil {
			return "", nil, err
		}
		name = strings.TrimSuffix(name, ".gz")
	}
	if len(data) > textLimit {
		return "", nil, errors.New("subtitle exceeds 512 KiB")
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		data, err = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if err != nil || len(data) > textLimit {
			return "", nil, errors.New("invalid subtitle encoding")
		}
	}
	if !utf8.Valid(data) {
		return "", nil, errors.New("subtitle must use UTF-8 or UTF-16")
	}
	cues, err := media.ParseSubtitles(string(data))
	if err != nil {
		return "", nil, errors.New("invalid downloaded subtitles")
	}
	if name == "" || name == "." {
		name = "OpenSubtitles.srt"
	}
	if len(name) > 240 {
		name = "OpenSubtitles.srt"
	}
	return name, cues, nil
}

func boundedRead(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, errors.New("subtitle read failed")
	}
	if len(data) > limit {
		return nil, errors.New("subtitle size limit exceeded")
	}
	return data, nil
}

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

type Client struct {
	Key, Token, BaseURL string
	Remaining           int
	ResetAt             time.Time
	VIP                 bool
	accountAt           time.Time
	redactions          []string
}
type Candidate struct {
	FileID                  int
	Name, Language, Release string
	Title, ParentTitle      string
	Downloads               int
	HearingImpaired         bool
	TMDBID                  int
	ParentTMDBID            int
	Season                  int
	Episode                 int
	Year                    int
}

// Provider errors deliberately omit response bodies, signed URLs and credentials.
func (c *Client) request(ctx context.Context, method, path string, body, target any) (resultErr error) {
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
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase()+path, input)
	if err != nil {
		return errors.New("invalid subtitle provider request")
	}
	started := time.Now()
	var res *http.Response
	defer func() { resultErr = c.requestError(resultErr, req, res, started, apiTimeout) }()
	req.Header.Set("Api-Key", c.Key)
	req.Header.Set("User-Agent", "JFE v0.1.0")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := http.Client{Transport: providerTransport, Timeout: apiTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err = client.Do(req)
	if err != nil {
		return c.connectionError(err, method, req.URL.Path, req.URL.Hostname())
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		err := c.responseError(res, method, req.URL.Path)
		var pe *Error
		if errors.As(err, &pe) {
			if pe.Remaining != nil {
				c.Remaining = *pe.Remaining
			}
			if !pe.ResetAt.IsZero() {
				c.ResetAt = pe.ResetAt
			}
		}
		return err
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(target); err != nil {
		if responseTimeout(err) || errors.Is(err, context.Canceled) {
			return c.connectionError(err, method, req.URL.Path, req.URL.Hostname())
		}
		return c.decodingError(err, res, method, req.URL.Path)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, query url.Values) ([]Candidate, error) {
	var result struct {
		Data []struct {
			Attributes struct {
				Feature struct {
					Title        string `json:"title"`
					MovieName    string `json:"movie_name"`
					ParentTitle  string `json:"parent_title"`
					TMDBID       int    `json:"tmdb_id"`
					ParentTMDBID int    `json:"parent_tmdb_id"`
					Season       int    `json:"season_number"`
					Episode      int    `json:"episode_number"`
					Year         int    `json:"year"`
				} `json:"feature_details"`
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
				title := row.Attributes.Feature.Title
				if title == "" {
					title = row.Attributes.Feature.MovieName
				}
				items = append(items, Candidate{FileID: f.ID, Name: f.Name, Language: row.Attributes.Language, Release: row.Attributes.Release, Title: title, ParentTitle: row.Attributes.Feature.ParentTitle, Downloads: row.Attributes.Downloads, HearingImpaired: row.Attributes.HearingImpaired, TMDBID: row.Attributes.Feature.TMDBID, ParentTMDBID: row.Attributes.Feature.ParentTMDBID, Season: row.Attributes.Feature.Season, Episode: row.Attributes.Feature.Episode, Year: row.Attributes.Feature.Year})
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

func (c *Client) Download(ctx context.Context, fileID int) (downloadName string, downloadCues []media.Cue, downloadErr error) {
	return c.download(ctx, fileID, false)
}

// DownloadManual tolerates individual non-positive-duration cues in the file
// explicitly selected by the user, without accepting invalid stored cues.
func (c *Client) DownloadManual(ctx context.Context, fileID int) (string, []media.Cue, error) {
	return c.download(ctx, fileID, true)
}

func (c *Client) download(ctx context.Context, fileID int, manual bool) (downloadName string, downloadCues []media.Cue, downloadErr error) {
	var result struct {
		Link      string `json:"link"`
		Name      string `json:"file_name"`
		Remaining *int   `json:"remaining"`
		Reset     string `json:"reset_time_utc"`
	}
	if err := c.request(ctx, "POST", "/download", struct {
		ID     int    `json:"file_id"`
		Format string `json:"sub_format"`
	}{fileID, "srt"}, &result); err != nil {
		return "", nil, err
	}
	if result.Remaining != nil {
		c.Remaining = *result.Remaining
	}
	if at := providerTime(result.Reset); !at.IsZero() {
		c.ResetAt = at
	}
	u, err := url.Parse(result.Link)
	if err != nil || !downloadURL(u) {
		return "", nil, errors.New("invalid subtitle download host")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", nil, errors.New("invalid subtitle download")
	}
	started := time.Now()
	var res *http.Response
	defer func() { downloadErr = c.requestError(downloadErr, req, res, started, downloadTimeout) }()
	client := http.Client{Transport: providerTransport, Timeout: downloadTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !downloadURL(req.URL) {
			return errors.New("invalid subtitle redirect")
		}
		return nil
	}}
	res, err = client.Do(req)
	if err != nil {
		return "", nil, c.connectionError(err, "GET", "subtitle_file", req.URL.Hostname())
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", nil, c.responseError(res, "GET", "subtitle_file")
	}
	data, err := boundedRead(res.Body, 4<<20)
	if err != nil {
		if responseTimeout(err) || errors.Is(err, context.Canceled) {
			return "", nil, c.connectionError(err, "GET", "subtitle_file", req.URL.Hostname())
		}
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
	parse := media.ParseSubtitles
	if manual {
		parse = media.ParseSelectedSubtitles
	}
	cues, err := parse(string(data))
	if err != nil {
		return "", nil, c.subtitleError(err, res, name, data)
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
		return nil, fmt.Errorf("subtitle read failed: %w", err)
	}
	if len(data) > limit {
		return nil, errors.New("subtitle size limit exceeded")
	}
	return data, nil
}

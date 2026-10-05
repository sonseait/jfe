// SPDX-License-Identifier: AGPL-3.0-or-later
// Naming and probe normalization adapted from Silo Server's internal/naming
// and internal/mediaprobe, revision 0362b6dae. See backend/NOTICE.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Scalar string

func (s *Scalar) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var v string
	if json.Unmarshal(data, &v) == nil {
		*s = Scalar(v)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*s = Scalar(n.String())
	return nil
}

type Stream struct {
	CodecTag         string `json:"codec_tag_string,omitempty"`
	SubtitleID       string `json:"subtitle_id,omitempty"`
	Profile          string `json:"profile,omitempty"`
	Level            int    `json:"level,omitempty"`
	BitsPerRawSample Scalar `json:"bits_per_raw_sample,omitempty"`
	FrameRate        Scalar `json:"avg_frame_rate,omitempty"`
	Channels         int    `json:"channels,omitempty"`
	SampleRate       Scalar `json:"sample_rate,omitempty"`
	ColorPrimaries   string `json:"color_primaries,omitempty"`
	ColorSpace       string `json:"color_space,omitempty"`
	ColorRange       string `json:"color_range,omitempty"`
	ColorTransfer    string `json:"color_transfer,omitempty"`
	SideData         []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list,omitempty"`
	BitRate      Scalar            `json:"bit_rate,omitempty"`
	PixelFormat  string            `json:"pix_fmt,omitempty"`
	ExternalPath string            `json:"external_path,omitempty"`
	Index        int               `json:"index"`
	Codec        string            `json:"codec_name"`
	Type         string            `json:"codec_type"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	Tags         map[string]string `json:"tags"`
	Disposition  struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}
type Probe struct {
	Version  int       `json:"probe_version,omitempty"`
	Streams  []Stream  `json:"streams"`
	Chapters []Chapter `json:"chapters"`
	Format   struct {
		Tags     map[string]string `json:"tags"`
		Duration Scalar            `json:"duration"`
		Name     string            `json:"format_name"`
	} `json:"format"`
}

func (p Probe) Duration() float64 {
	v, _ := strconv.ParseFloat(string(p.Format.Duration), 64)
	return v
}
func Inspect(ctx context.Context, path string) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-show_chapters", "-of", "json", path)
	b, err := cmd.CombinedOutput()
	var p Probe
	if err != nil {
		diagnostic := strings.Join(strings.Fields(string(b)), " ")
		if len(diagnostic) > 2048 {
			diagnostic = diagnostic[:2048]
		}
		if diagnostic != "" {
			return p, fmt.Errorf("ffprobe failed: %w: %s", err, diagnostic)
		}
		return p, fmt.Errorf("ffprobe failed: %w", err)
	}
	err = json.Unmarshal(b, &p)
	p.Version = 3
	return p, err
}
func Within(root, path string) (string, error) {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	r, _ = filepath.Abs(r)
	p, _ = filepath.Abs(p)
	rel, err := filepath.Rel(r, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside media root")
	}
	return p, nil
}

var episodePattern = regexp.MustCompile(`(?i)s(\d{1,4})e(\d+)`)
var yearPattern = regexp.MustCompile(`(?:^|[ ._(])((?:19|20)\d{2})(?:[ ._)\-]|$)`)
var releasePattern = regexp.MustCompile(`(?i)[ ._-](?:2160p|1080p|720p|480p|bluray|web-dl|webrip|hdtv|x264|x265|h264|h265|hevc).*$`)

type Name struct {
	Title                 string
	Year, Season, Episode int
	Series                string
}

func Parse(path string) Name {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	n := Name{}
	if loc := episodePattern.FindStringSubmatchIndex(base); loc != nil {
		n.Season, _ = strconv.Atoi(base[loc[2]:loc[3]])
		digits := base[loc[4]:loc[5]]
		if len(digits) <= 5 {
			n.Episode, _ = strconv.Atoi(digits)
		}
		n.Series = clean(base[:loc[0]])
		if n.Series == "" {
			dir := filepath.Dir(path)
			if strings.HasPrefix(strings.ToLower(filepath.Base(dir)), "season") {
				dir = filepath.Dir(dir)
			}
			n.Series = clean(filepath.Base(dir))
		}
		n.Title = fmt.Sprintf("%s S%02dE%02d", n.Series, n.Season, n.Episode)
		return n
	}
	if loc := yearPattern.FindStringSubmatchIndex(base); loc != nil {
		n.Year, _ = strconv.Atoi(base[loc[2]:loc[3]])
		base = base[:loc[0]]
	}
	n.Title = clean(releasePattern.ReplaceAllString(base, ""))
	return n
}
func clean(s string) string {
	return strings.Trim(strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(s)), " "), " -()[]")
}
func IsVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".webm", ".ts":
		return true
	}
	return false
}

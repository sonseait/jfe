package media

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Cue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

var subtitleTime = regexp.MustCompile(`^(?:(\d{1,3}):)?(\d{2}):(\d{2})[.,](\d{3})$`)
var subtitleTags = regexp.MustCompile(`<[^>]*>`)

func cueTime(value string) (float64, error) {
	m := subtitleTime.FindStringSubmatch(value)
	if m == nil {
		return 0, fmt.Errorf("invalid timestamp")
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec, _ := strconv.Atoi(m[3])
	ms, _ := strconv.Atoi(m[4])
	if min > 59 || sec > 59 {
		return 0, fmt.Errorf("invalid timestamp")
	}
	return float64(h*3600+min*60+sec) + float64(ms)/1000, nil
}

// ParseSubtitles accepts UTF-8 SRT/WebVTT and stores plain-text cues, never markup.
func ParseSubtitles(content string) ([]Cue, error) {
	if len(content) > 512*1024 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return nil, fmt.Errorf("invalid subtitle encoding or size")
	}
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(content, "\n")
	cues := []Cue{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "NOTE" || strings.HasPrefix(line, "NOTE ") || line == "STYLE" || line == "REGION" {
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				i++
			}
			continue
		}
		if !strings.Contains(line, "-->") {
			continue
		}
		parts := strings.Split(line, "-->")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid cue")
		}
		endParts := strings.Fields(parts[1])
		if len(endParts) == 0 {
			return nil, fmt.Errorf("invalid cue")
		}
		start, err := cueTime(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, err
		}
		end, err := cueTime(endParts[0])
		if err != nil || end <= start {
			return nil, fmt.Errorf("invalid cue duration")
		}
		text := []string{}
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			text = append(text, lines[i])
		}
		plain := strings.TrimSpace(html.UnescapeString(subtitleTags.ReplaceAllString(strings.Join(text, "\n"), "")))
		if plain == "" {
			continue
		}
		if len(plain) > 8192 {
			return nil, fmt.Errorf("cue too long")
		}
		cues = append(cues, Cue{Start: start, End: end, Text: plain})
		if len(cues) > 20000 {
			return nil, fmt.Errorf("too many cues")
		}
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("no subtitle cues")
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}

// SubtitlesASS serializes stored plain-text cues directly for libass playback.
// Do not pass these through SRT: FFmpeg's SRT decoder preserves HTML entities.
func SubtitlesASS(cues []Cue) (string, error) {
	if len(cues) == 0 || len(cues) > 20000 {
		return "", fmt.Errorf("invalid subtitle cues")
	}
	timestamp := func(cs int64) string {
		return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
	}
	var content strings.Builder
	content.WriteString("[Script Info]\nScriptType: v4.00+\nPlayResX: 384\nPlayResY: 288\nScaledBorderAndShadow: yes\nYCbCr Matrix: None\n\n" +
		"[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,16,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1\n\n" +
		"[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, cue := range cues {
		if math.IsNaN(cue.Start) || math.IsNaN(cue.End) || math.IsInf(cue.Start, 0) || math.IsInf(cue.End, 0) || cue.Start < 0 || cue.End <= cue.Start || cue.End > 3600000 || len(cue.Text) > 8192 || !utf8.ValidString(cue.Text) || strings.ContainsRune(cue.Text, 0) {
			return "", fmt.Errorf("invalid subtitle cue")
		}
		text := strings.ReplaceAll(strings.ReplaceAll(cue.Text, "\r\n", "\n"), "\r", "\n")
		// libass has no escape for a literal backslash. An invisible word joiner
		// prevents user text such as \N or \h from becoming ASS controls.
		// Escape braces too, so cue text cannot inject style/drawing commands.
		text = strings.NewReplacer("\\", "\\\u2060", "{", "\\{", "}", "\\}", "\n", "\\N").Replace(text)
		start, end := int64(math.Round(cue.Start*100)), int64(math.Round(cue.End*100))
		if end <= start {
			end = start + 1
		}
		fmt.Fprintf(&content, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", timestamp(start), timestamp(end), text)
	}
	return content.String(), nil
}

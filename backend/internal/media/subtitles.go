package media

import (
	"fmt"
	"html"
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

package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrSubtitleNegative = errors.New("subtitle_negative_time")
var ErrSubtitleFormat = errors.New("subtitle_unsupported_format")
var assTime = regexp.MustCompile(`^(\d+):(\d{2}):(\d{2})\.(\d{2})$`)
var timingLine = regexp.MustCompile(`(?m)^([ \t]*)(\d{1,3}:\d{2}:\d{2}[.,]\d{3}|\d{2}:\d{2}[.,]\d{3})([ \t]+-->[ \t]+)(\d{1,3}:\d{2}:\d{2}[.,]\d{3}|\d{2}:\d{2}[.,]\d{3})([^\r\n]*)`)

func SubtitleTextCodec(codec string) bool {
	switch codec {
	case "subrip", "srt", "webvtt", "vtt", "ass", "ssa":
		return true
	}
	return false
}

// ShiftSubtitleDocument changes only timing fields; text, styling, cue settings,
// metadata, BOM and line endings remain byte-for-byte intact.
func ShiftSubtitleDocument(content, format string, offsetMS int) (string, error) {
	if len(content) > 512*1024 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) || offsetMS < -600000 || offsetMS > 600000 {
		return "", ErrSubtitleFormat
	}
	if format == "ass" || format == "ssa" {
		return shiftASS(content, offsetMS)
	}
	if format != "srt" && format != "vtt" && format != "webvtt" && format != "subrip" {
		return "", ErrSubtitleFormat
	}
	if _, err := ParseSubtitles(content); err != nil {
		return "", err
	}
	var failed error
	result := timingLine.ReplaceAllStringFunc(content, func(line string) string {
		m := timingLine.FindStringSubmatch(line)
		start, e := cueTime(m[2])
		if e != nil {
			failed = e
			return line
		}
		end, e := cueTime(m[4])
		if e != nil {
			failed = e
			return line
		}
		a, b := int64(math.Round(start*1000))+int64(offsetMS), int64(math.Round(end*1000))+int64(offsetMS)
		if a < 0 || b <= a {
			failed = ErrSubtitleNegative
			return line
		}
		return m[1] + subtitleTimestamp(a, strings.Contains(m[2], ",")) + m[3] + subtitleTimestamp(b, strings.Contains(m[4], ",")) + m[5]
	})
	return result, failed
}
func subtitleTimestamp(ms int64, comma bool) string {
	sep := "."
	if comma {
		sep = ","
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, sep, ms%1000)
}
func assMilliseconds(value string) (int64, error) {
	m := assTime.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return 0, ErrSubtitleFormat
	}
	h, _ := strconv.ParseInt(m[1], 10, 64)
	min, _ := strconv.ParseInt(m[2], 10, 64)
	sec, _ := strconv.ParseInt(m[3], 10, 64)
	cs, _ := strconv.ParseInt(m[4], 10, 64)
	if h > 999 || min > 59 || sec > 59 {
		return 0, ErrSubtitleFormat
	}
	return h*3600000 + min*60000 + sec*1000 + cs*10, nil
}
func shiftASS(content string, offset int) (string, error) {
	// ASS stores centiseconds. Never silently round a requested offset.
	if offset%10 != 0 {
		return "", ErrSubtitleFormat
	}
	lines := strings.SplitAfter(content, "\n")
	inEvents := false
	start, end, count := -1, -1, 0
	for i, line := range lines {
		trim := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if strings.HasPrefix(trim, "[") {
			inEvents = strings.EqualFold(trim, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		kind := strings.TrimSpace(line[:colon])
		if strings.EqualFold(kind, "Format") {
			start, end = -1, -1
			for n, v := range strings.Split(line[colon+1:], ",") {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "start":
					start = n
				case "end":
					end = n
				}
			}
		}
		if !strings.EqualFold(kind, "Dialogue") && !strings.EqualFold(kind, "Comment") {
			continue
		}
		if start < 0 || end < 0 {
			return "", ErrSubtitleFormat
		}
		fields := strings.SplitN(line[colon+1:], ",", max(start, end)+2)
		if len(fields) <= max(start, end) {
			return "", ErrSubtitleFormat
		}
		for _, n := range []int{start, end} {
			v, e := assMilliseconds(fields[n])
			if e != nil {
				return "", e
			}
			v += int64(offset)
			if v < 0 {
				return "", ErrSubtitleNegative
			}
			old := fields[n]
			left := old[:len(old)-len(strings.TrimLeft(old, " \t"))]
			right := old[len(strings.TrimRight(old, " \t")):]
			fields[n] = left + fmt.Sprintf("%d:%02d:%02d.%02d", v/3600000, v/60000%60, v/1000%60, v/10%100) + right
		}
		lines[i] = line[:colon+1] + strings.Join(fields, ",")
		count++
	}
	if count == 0 {
		return "", ErrSubtitleFormat
	}
	return strings.Join(lines, ""), nil
}

// Sidecar management must not turn a subtitle symlink into permission to delete
// an arbitrary media file. The resolved source must remain a matching sidecar.
func SubtitleSidecarSource(video, source string) bool {
	if filepath.Dir(video) != filepath.Dir(source) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(source))
	if ext != ".srt" && ext != ".vtt" && ext != ".ass" && ext != ".ssa" {
		return false
	}
	base := strings.TrimSuffix(video, filepath.Ext(video))
	stem := strings.TrimSuffix(source, filepath.Ext(source))
	return stem == base || strings.HasPrefix(stem, base+".")
}

func SubtitleProbeRevision(probe []byte) string {
	h := sha256.Sum256(probe)
	return hex.EncodeToString(h[:])
}

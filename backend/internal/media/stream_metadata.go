package media

import (
	"strconv"
	"strings"
)

func (s Stream) BitDepth() int {
	if n, _ := strconv.Atoi(string(s.BitsPerRawSample)); n > 0 {
		return n
	}
	for _, n := range []string{"16", "14", "12", "10", "9"} {
		if strings.Contains(s.PixelFormat, "p"+n) {
			v, _ := strconv.Atoi(n)
			return v
		}
	}
	if s.PixelFormat != "" {
		return 8
	}
	return 0
}
func (s Stream) HDRFormat() string {
	dynamic := false
	for _, d := range s.SideData {
		if strings.Contains(d.Type, "DOVI") {
			return "dolby_vision"
		}
		if strings.Contains(d.Type, "HDR Dynamic Metadata") {
			dynamic = true
		}
	}
	if dynamic {
		return "hdr10plus"
	}
	switch s.ColorTransfer {
	case "smpte2084":
		return "hdr10"
	case "arib-std-b67":
		return "hlg"
	}
	return ""
}
func (s Stream) FPS() float64 {
	parts := strings.Split(string(s.FrameRate), "/")
	n, _ := strconv.ParseFloat(parts[0], 64)
	if len(parts) == 2 {
		d, _ := strconv.ParseFloat(parts[1], 64)
		if d == 0 {
			return 0
		}
		return n / d
	}
	return n
}
func (s Stream) SubtitleType() string {
	switch s.Codec {
	case "hdmv_pgs_subtitle", "dvd_subtitle":
		return "bitmap"
	case "subrip", "srt", "webvtt", "ass", "ssa", "mov_text":
		return "text"
	}
	return "unknown"
}

// DolbyVisionHDR10Base requires an explicitly signaled, single-layer profile 8
// HDR10 base. Profile 5 and enhancement-layer streams need RPU-aware processing.
// Keep HDRFormat as Dolby Vision for direct-play capability negotiation.
func (s Stream) DolbyVisionHDR10Base() bool {
	if s.Codec != "hevc" || s.ColorTransfer != "smpte2084" {
		return false
	}
	found := false
	for _, d := range s.SideData {
		if !strings.Contains(d.Type, "DOVI") {
			continue
		}
		if d.Type != "DOVI configuration record" || d.DVProfile != 8 || d.BLCompatibilityID != 1 || d.BLPresent != 1 || d.ELPresent == nil || *d.ELPresent != 0 {
			return false
		}
		found = true
	}
	return found
}

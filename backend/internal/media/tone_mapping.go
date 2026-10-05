package media

import (
	"fmt"
	"strings"
)

// ToneMappingFilter converts the static HDR base image to limited-range BT.709
// SDR. It is a CPU filter chain; playback video encoding must still use NVENC.
// Dolby Vision requires RPU-aware processing and cannot use this base-image path.
func ToneMappingFilter(s Stream) (string, error) {
	hdr := s.HDRFormat()
	if hdr == "" {
		return "", nil
	}
	transfer := "smpte2084"
	switch hdr {
	case "hlg":
		transfer = "arib-std-b67"
	case "hdr10", "hdr10plus":
		if s.ColorTransfer != "" && s.ColorTransfer != "smpte2084" {
			return "", fmt.Errorf("HDR10 requires PQ transfer metadata")
		}
	default:
		return "", fmt.Errorf("unsupported HDR tone mapping: %s", hdr)
	}
	primaries := s.ColorPrimaries
	if primaries == "" || primaries == "unknown" {
		primaries = "bt2020"
	}
	// Whitelist FFmpeg values; media metadata must never inject a filter graph.
	if !containsColor([]string{"bt2020", "bt709", "smpte431", "smpte432"}, primaries) {
		return "", fmt.Errorf("unsupported HDR color primaries: %s", primaries)
	}
	matrix := s.ColorSpace
	if matrix == "" || matrix == "unknown" {
		matrix = "bt2020nc"
	}
	if !containsColor([]string{"bt2020nc", "bt2020c", "bt709"}, matrix) {
		return "", fmt.Errorf("unsupported HDR color matrix: %s", matrix)
	}
	inputRange := "limited"
	switch s.ColorRange {
	case "pc", "jpeg":
		inputRange = "full"
	case "", "unknown", "tv", "mpeg":
	default:
		return "", fmt.Errorf("unsupported HDR color range: %s", s.ColorRange)
	}
	return strings.Join([]string{
		fmt.Sprintf("zscale=primariesin=%s:transferin=%s:matrixin=%s:rangein=%s:transfer=linear:npl=100", primaries, transfer, matrix, inputRange),
		"format=gbrpf32le",
		"zscale=primaries=bt709",
		// Auto peak uses mastering/content-light frame metadata when available.
		"tonemap=tonemap=hable:desat=2:peak=0",
		"zscale=transfer=bt709:matrix=bt709:range=limited",
		"format=yuv420p",
		// Misreported source peak metadata must not produce out-of-range SDR.
		"limiter=min=16:max=235:planes=1",
		"limiter=min=16:max=240:planes=6",
		"sidedata=mode=delete:type=MASTERING_DISPLAY_METADATA",
		"sidedata=mode=delete:type=CONTENT_LIGHT_LEVEL",
		"sidedata=mode=delete:type=DYNAMIC_HDR_PLUS",
	}, ","), nil
}
func containsColor(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func SDRColorArgs() []string {
	return []string{"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv"}
}

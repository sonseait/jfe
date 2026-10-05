package media

import (
	"fmt"
	"strings"
)

// ToneMappingFilter converts the static HDR base image to limited-range BT.709
// SDR on CUDA frames. Playback requires NVIDIA decode, tone mapping and NVENC;
// there is no software tone-mapping fallback.
// Compatible single-layer Dolby Vision profile 8 uses only its HDR10 base;
// Dolby Vision dynamic metadata is not applied. Other profiles remain rejected.
func ToneMappingFilter(s Stream) (string, error) {
	hdr := s.HDRFormat()
	if hdr == "" {
		return "", nil
	}
	transfer := "smpte2084"
	switch hdr {
	case "dolby_vision":
		if !s.DolbyVisionHDR10Base() {
			return "", fmt.Errorf("Dolby Vision has no supported HDR10 base")
		}
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
	filters := []string{
		// setparams changes frame metadata only, leaving pixels on the GPU.
		fmt.Sprintf("setparams=range=%s:color_primaries=%s:color_trc=%s:colorspace=%s", inputRange, primaries, transfer, matrix),
		// Use the HDR10 base only, even if a decoder provides Dolby Vision RPU.
		"tonemap_cuda=tonemap=hable:desat=2:peak=0:format=nv12:primaries=bt709:transfer=bt709:matrix=bt709:range=tv:apply_dovi=0",
		"sidedata=mode=delete:type=MASTERING_DISPLAY_METADATA",
		"sidedata=mode=delete:type=CONTENT_LIGHT_LEVEL",
		"sidedata=mode=delete:type=DYNAMIC_HDR_PLUS",
		"sidedata=mode=delete:type=DOVI_RPU_BUFFER",
		"sidedata=mode=delete:type=DOVI_METADATA",
	}
	return strings.Join(filters, ","), nil
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

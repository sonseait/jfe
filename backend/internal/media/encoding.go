package media

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Encoding permits NVENC video encoding only. Unknown modes fail closed.
type Encoding struct {
	Mode                      string  `json:"mode"`
	CQ                        int     `json:"cq"`
	Device                    int     `json:"device"`
	MaxConcurrent             int     `json:"maxConcurrent"`
	Preset                    string  `json:"preset"`
	VideoCodec                string  `json:"videoCodec"`
	Bitrate720                int     `json:"bitrate720"`
	Bitrate1080               int     `json:"bitrate1080"`
	Bitrate2160               int     `json:"bitrate2160"`
	AudioCodec                string  `json:"audioCodec"`
	AudioBitrate              int     `json:"audioBitrate"`
	SubtitleSize              int     `json:"subtitleSize"`
	SubtitleOutline           float64 `json:"subtitleOutline"`
	SubtitleMargin            int     `json:"subtitleMargin"`
	SubtitleFont              string  `json:"subtitleFont"`
	SubtitleColor             string  `json:"subtitleColor"`
	SubtitleBackground        string  `json:"subtitleBackground"`
	SubtitleBackgroundOpacity int     `json:"subtitleBackgroundOpacity"`
	SubtitleBorderColor       string  `json:"subtitleBorderColor"`
}

func ParseEncoding(data []byte) (Encoding, error) {
	cfg := Encoding{Mode: "disabled", CQ: 23, MaxConcurrent: 1, Preset: "p4", VideoCodec: "h264", Bitrate720: 4000000, Bitrate1080: 8000000, Bitrate2160: 20000000, AudioCodec: "aac", AudioBitrate: 192000, SubtitleSize: 24, SubtitleOutline: 2, SubtitleMargin: 32, SubtitleFont: "Arial", SubtitleColor: "#FFFFFF", SubtitleBackground: "#000000", SubtitleBorderColor: "#000000"}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Mode != "disabled" && cfg.Mode != "nvidia" {
		return cfg, fmt.Errorf("unsupported video encoder mode")
	}
	if cfg.CQ < 0 || cfg.CQ > 51 || cfg.Device < 0 || cfg.Device > 31 || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 16 ||
		(cfg.Preset != "p1" && cfg.Preset != "p2" && cfg.Preset != "p3" && cfg.Preset != "p4" && cfg.Preset != "p5" && cfg.Preset != "p6" && cfg.Preset != "p7") ||
		(cfg.VideoCodec != "h264" && cfg.VideoCodec != "hevc") ||
		cfg.Bitrate720 < 100000 || cfg.Bitrate720 > 100000000 || cfg.Bitrate1080 < 100000 || cfg.Bitrate1080 > 100000000 || cfg.Bitrate2160 < 100000 || cfg.Bitrate2160 > 100000000 ||
		(cfg.AudioCodec != "aac" && cfg.AudioCodec != "ac3") || cfg.AudioBitrate < 64000 || cfg.AudioBitrate > 1024000 ||
		cfg.SubtitleSize < 12 || cfg.SubtitleSize > 72 || cfg.SubtitleOutline < 0 || cfg.SubtitleOutline > 10 || cfg.SubtitleOutline != math.Round(cfg.SubtitleOutline*2)/2 || cfg.SubtitleMargin < 0 || cfg.SubtitleMargin > 200 || cfg.SubtitleBackgroundOpacity < 0 || cfg.SubtitleBackgroundOpacity > 100 ||
		(cfg.SubtitleFont != "Arial" && cfg.SubtitleFont != "Noto Sans" && cfg.SubtitleFont != "Noto Sans CJK") || !validColor(cfg.SubtitleColor) || !validColor(cfg.SubtitleBackground) || !validColor(cfg.SubtitleBorderColor) {
		return cfg, fmt.Errorf("invalid encoding settings")
	}
	return cfg, nil
}

func validColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 24)
	return err == nil
}

// SubtitleStyle returns a constrained ASS style for FFmpeg's subtitles filter.
func (cfg Encoding) SubtitleStyle() string {
	backgroundStyle := "BorderStyle=1"
	if cfg.SubtitleBackgroundOpacity > 0 {
		backgroundStyle = "BorderStyle=3"
	}
	return strings.Join([]string{
		"FontName=" + cfg.SubtitleFont,
		"FontSize=" + strconv.Itoa(cfg.SubtitleSize),
		"PrimaryColour=" + assColor(cfg.SubtitleColor, 100),
		"OutlineColour=" + assColor(cfg.SubtitleBorderColor, 100),
		"BackColour=" + assColor(cfg.SubtitleBackground, cfg.SubtitleBackgroundOpacity),
		"Outline=" + strconv.FormatFloat(cfg.SubtitleOutline, 'f', -1, 64),
		"MarginV=" + strconv.Itoa(cfg.SubtitleMargin),
		backgroundStyle,
	}, ",")
}

func assColor(hex string, opacity int) string {
	v, _ := strconv.ParseUint(hex[1:], 16, 24)
	red, green, blue := byte(v>>16), byte(v>>8), byte(v)
	alpha := byte(255 - opacity*255/100)
	return fmt.Sprintf("&H%02X%02X%02X%02X", alpha, blue, green, red)
}
func (cfg Encoding) VideoArgs(bitrate int) ([]string, error) {
	if cfg.Mode != "nvidia" {
		return nil, fmt.Errorf("video transcoding is disabled")
	}
	codec := "h264_nvenc"
	if cfg.VideoCodec == "hevc" {
		codec = "hevc_nvenc"
	}
	return []string{"-c:v", codec, "-gpu", strconv.Itoa(cfg.Device), "-preset", cfg.Preset, "-rc", "vbr", "-cq", strconv.Itoa(cfg.CQ), "-b:v", strconv.Itoa(bitrate), "-maxrate", strconv.Itoa(bitrate), "-bufsize", strconv.Itoa(bitrate * 2), "-pix_fmt", "yuv420p"}, nil
}

func (cfg Encoding) VideoBitrate(height, limit int) int {
	bitrate := cfg.Bitrate2160
	if height == 720 {
		bitrate = cfg.Bitrate720
	} else if height == 1080 {
		bitrate = cfg.Bitrate1080
	}
	if limit > 0 && limit < bitrate {
		return limit
	}
	return bitrate
}

// CUDAInputArgs creates one selected device for decode, filters and uploads.
// Used by all video transcodes; direct play/remux never require a GPU.
func (cfg Encoding) CUDAInputArgs() ([]string, error) {
	if cfg.Mode != "nvidia" {
		return nil, fmt.Errorf("video transcoding is disabled")
	}
	return []string{"-init_hw_device", "cuda=jfe:" + strconv.Itoa(cfg.Device), "-filter_hw_device", "jfe", "-hwaccel", "cuda", "-hwaccel_device", "jfe", "-hwaccel_output_format", "cuda"}, nil
}

package server

import (
	"jfe/backend/internal/media"
	"path/filepath"
	"strconv"
	"strings"
)

func trackDTO(t media.Stream) TrackDTO {
	bitrate, _ := strconv.ParseInt(string(t.BitRate), 10, 64)
	sampleRate, _ := strconv.Atoi(string(t.SampleRate))
	subtitleType := ""
	if t.Type == "subtitle" {
		subtitleType = t.SubtitleType()
	}
	return TrackDTO{CodecTag: t.CodecTag, SubtitleID: t.SubtitleID, ExternalSubtitle: t.SubtitleID != "", Index: t.Index, Type: t.Type, Codec: t.Codec, Language: t.Tags["language"], Title: t.Tags["title"], Profile: t.Profile, Level: t.Level, BitDepth: t.BitDepth(), PixelFormat: t.PixelFormat, Width: t.Width, Height: t.Height, Bitrate: bitrate, FrameRate: t.FPS(), HDRFormat: t.HDRFormat(), Channels: t.Channels, SampleRate: sampleRate, SubtitleType: subtitleType}
}

func sourceContainer(p media.Probe, path string) string {
	if strings.Contains(p.Format.Name, "mp4") {
		return "mp4"
	}
	// ffprobe shares the Matroska/WebM demuxer; distinguish its WebM subset.
	if strings.Contains(p.Format.Name, "webm") && strings.EqualFold(filepath.Ext(path), ".webm") {
		return "webm"
	}
	if strings.Contains(p.Format.Name, "matroska") {
		return "mkv"
	}
	return p.Format.Name
}

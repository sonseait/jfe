package server

import (
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"strings"
)

// DirectPlay reports browser support for the original container and codec pair.
// Quality limits are ceilings: a smaller source does not need re-encoding.
func playbackMethod(probe media.Probe, b PlaybackRequest, size int64, duration float64) (string, error) {
	var video *media.Stream
	validAudio, validSub := b.AudioIndex == -1, b.SubtitleIndex == -1
	for i := range probe.Streams {
		track := &probe.Streams[i]
		if video == nil && track.Type == "video" && track.Disposition.AttachedPic == 0 {
			video = track
		}
		if track.Type == "audio" && track.Index == b.AudioIndex {
			validAudio = true
		}
		if track.Type == "subtitle" && track.Index == b.SubtitleIndex {
			validSub = true
		}
	}
	if !validAudio || !validSub {
		return "", route.Fail(422, "Invalid media track")
	}
	if video == nil {
		hasAudio := false
		for _, t := range probe.Streams {
			if t.Type == "audio" {
				hasAudio = true
			}
		}
		if !hasAudio {
			return "", route.Fail(422, "No audio or video stream")
		}
		if b.SubtitleIndex != -1 || b.SubtitleID != "" {
			return "", route.Fail(422, "Audio does not support subtitles")
		}
		if b.DirectPlay && !b.ForceTranscode {
			return "direct", nil
		}
		return "audio", nil
	}
	bitrate := float64(0)
	if duration > 0 {
		bitrate = float64(size) * 8 / duration
	}
	maxWidth := map[int]int{360: 640, 720: 1280, 1080: 1920, 2160: 3840}[b.MaxHeight]
	resize := b.MaxHeight > 0 && (video.Height == 0 || video.Height > b.MaxHeight || (maxWidth > 0 && video.Width > maxWidth))
	reduceBitrate := b.MaxBitrate > 0 && (bitrate == 0 || bitrate > float64(b.MaxBitrate))
	if b.ForceTranscode || (b.SubtitleIndex != -1 || b.SubtitleID != "") || resize || reduceBitrate {
		return "transcode", nil
	}
	container := strings.Contains(probe.Format.Name, "mp4") || strings.Contains(probe.Format.Name, "webm")
	if b.DirectPlay && b.AudioIndex == -1 && container {
		return "direct", nil
	}
	if video.Codec == "h264" && (video.PixelFormat == "" || video.PixelFormat == "yuv420p") {
		return "remux", nil
	}
	return "transcode", nil
}

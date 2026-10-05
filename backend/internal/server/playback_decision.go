package server

import (
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"slices"
	"strings"
)

type PlaybackMode string

const (
	PlaybackModeDirectPlay PlaybackMode = "direct_play"
	PlaybackModeRemux      PlaybackMode = "remux"
	PlaybackModeTranscode  PlaybackMode = "transcode"
)

// Legacy callers retain their original behavior. New clients report measured
// support for the source profile, depth, level, dimensions and dynamic range.
func playbackDecision(probe media.Probe, b PlaybackRequest, size int64, duration float64) (PlaybackDecisionDTO, error) {
	legacy, err := playbackMethod(probe, b, size, duration) // also validates track selection
	if err != nil {
		return PlaybackDecisionDTO{}, err
	}
	d := PlaybackDecisionDTO{VideoAction: "copy", AudioAction: "copy", SubtitleAction: "none", SubtitleID: b.SubtitleID, Container: "mp4"}
	if b.Capabilities == nil || legacy == "audio" {
		d.Reason = "Legacy playback capability report"
		switch legacy {
		case "direct":
			d.Mode = PlaybackModeDirectPlay
			d.Container = "original"
		case "remux", "audio":
			d.Mode = PlaybackModeRemux
			d.Container = "hls"
			d.AudioAction = "transcode"
			if legacy == "remux" {
				for _, track := range probe.Streams {
					if track.Type == "audio" && (b.AudioIndex == -1 || track.Index == b.AudioIndex) {
						if track.Codec == "aac" {
							d.AudioAction = "copy"
						}
						break
					}
				}
			}
		default:
			d.Mode = PlaybackModeTranscode
			d.Container = "hls"
			if b.SubtitleIndex >= 0 || b.SubtitleID != "" {
				d.SubtitleAction = "burn"
			}
			d.VideoAction = "transcode"
			d.AudioAction = "transcode"
		}
		return applyToneMappingDecision(probe, d)
	}
	var video, audio *media.Stream
	for i := range probe.Streams {
		t := &probe.Streams[i]
		if t.Type == "video" && t.Disposition.AttachedPic == 0 && video == nil {
			video = t
		}
		if t.Type == "audio" && ((b.AudioIndex == -1 && audio == nil) || t.Index == b.AudioIndex) {
			audio = t
		}
	}
	if video == nil {
		return d, route.Fail(422, "No video stream")
	}
	c := b.Capabilities
	supported := false
	for _, v := range c.Video {
		if v.Codec == video.Codec && strings.EqualFold(v.Profile, video.Profile) && v.BitDepth == video.BitDepth() && v.Level >= video.Level && v.MaxWidth >= video.Width && v.MaxHeight >= video.Height && v.HDRFormat == video.HDRFormat() {
			supported = true
			break
		}
	}
	bitrate := float64(0)
	if duration > 0 {
		bitrate = float64(size) * 8 / duration
	}
	maxWidth := map[int]int{360: 640, 720: 1280, 1080: 1920, 2160: 3840}[b.MaxHeight]
	resize := b.MaxHeight > 0 && (video.Height == 0 || video.Height > b.MaxHeight || video.Width > maxWidth)
	reduce := b.MaxBitrate > 0 && (bitrate == 0 || bitrate > float64(b.MaxBitrate))
	if b.SubtitleIndex >= 0 || b.SubtitleID != "" {
		d.SubtitleAction = "burn"
	}
	reason := ""
	switch {
	case b.ForceTranscode:
		reason = "Client requested video transcoding after a playback failure"
	case b.SubtitleIndex >= 0 || b.SubtitleID != "":
		reason = "Selected subtitle requires burn-in"
		d.SubtitleAction = "burn"
	case resize || reduce:
		reason = "Source exceeds playback quality or bandwidth ceiling"
	case !supported:
		reason = "Source video codec, profile, bit depth, level or HDR is unsupported"
	}
	if reason != "" {
		d.Mode = PlaybackModeTranscode
		d.VideoCodec = "h264"
		d.VideoAction = "transcode"
		d.AudioAction = "transcode"
		d.Container = "hls"
		d.Reason = reason
		return applyToneMappingDecision(probe, d)
	}
	audioSupported := audio == nil || slices.Contains(c.Audio, audio.Codec)
	container := ""
	if strings.Contains(probe.Format.Name, "mp4") {
		container = "mp4"
	} else if strings.Contains(probe.Format.Name, "webm") {
		container = "webm"
	}
	if b.DirectPlay && b.AudioIndex == -1 && audioSupported && slices.Contains(c.Containers, container) && container != "" {
		d.Mode = PlaybackModeDirectPlay
		d.Container = container
		d.Reason = "Source container and selected codecs are supported"
		return d, nil
	}
	if !c.Remux {
		return d, route.Fail(409, "Client cannot play fragmented MP4 remux")
	}
	d.Mode = PlaybackModeRemux
	d.Reason = "Video is supported; container or track selection requires remux"
	// Native file and MSE audio support can differ (for example AC3).
	remuxAudio := c.RemuxAudio
	if remuxAudio == nil {
		remuxAudio = c.Audio
	}
	audioSupported = audio == nil || slices.Contains(remuxAudio, audio.Codec)
	// Only copy codecs supported by the client and MP4 muxer.
	if audio != nil && (!audioSupported || !slices.Contains([]string{"aac", "mp3", "ac3", "eac3", "opus", "flac"}, audio.Codec)) {
		if !slices.Contains(remuxAudio, "aac") {
			return d, route.Fail(409, "Client cannot play AAC remux audio")
		}
		d.AudioAction = "transcode"
		d.Reason = "Video is supported; selected audio requires AAC conversion"
	}
	return d, nil
}

func applyToneMappingDecision(probe media.Probe, d PlaybackDecisionDTO) (PlaybackDecisionDTO, error) {
	if d.Mode != PlaybackModeTranscode {
		return d, nil
	}
	for _, track := range probe.Streams {
		if track.Type != "video" || track.Disposition.AttachedPic != 0 {
			continue
		}
		filter, err := media.ToneMappingFilter(track)
		if err != nil {
			return d, route.Fail(409, "HDR video transcoding is unavailable")
		}
		if filter != "" {
			d.ToneMapped = true
			d.VideoCodec = "h264"
			d.Reason += "; HDR converted to BT.709 SDR"
		}
		break
	}
	return d, nil
}

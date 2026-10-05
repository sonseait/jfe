package server

import (
	"jfe/backend/internal/media"
	"testing"
)

func TestPlaybackDecisionPreservesHEVC(t *testing.T) {
	for _, tc := range []struct {
		name, container, audio, profile string
		depth                           int
		supported, burn                 bool
		mode                            PlaybackMode
		audioAction                     string
	}{
		{"MP4 HEVC AAC", "mp4", "aac", "Main", 8, true, false, PlaybackModeDirectPlay, "copy"},
		{"MKV HEVC AAC", "matroska", "aac", "Main", 8, true, false, PlaybackModeRemux, "copy"},
		{"MKV HEVC TrueHD", "matroska", "truehd", "Main", 8, true, false, PlaybackModeRemux, "transcode"},
		{"MKV HEVC DTS", "matroska", "dts", "Main", 8, true, false, PlaybackModeRemux, "transcode"},
		{"unsupported HEVC", "mp4", "aac", "Main", 8, false, false, PlaybackModeTranscode, "transcode"},
		{"PGS burn", "matroska", "aac", "Main", 8, true, true, PlaybackModeTranscode, "transcode"},
		{"Main10 supported", "matroska", "aac", "Main 10", 10, true, false, PlaybackModeRemux, "copy"},
		{"Main10 unsupported", "mp4", "aac", "Main 10", 10, false, false, PlaybackModeTranscode, "transcode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pixel := "yuv420p"
			if tc.depth == 10 {
				pixel = "yuv420p10le"
			}
			p := media.Probe{Streams: []media.Stream{{Index: 0, Type: "video", Codec: "hevc", Profile: tc.profile, Level: 153, PixelFormat: pixel, Width: 3840, Height: 2160}, {Index: 1, Type: "audio", Codec: tc.audio}, {Index: 2, Type: "subtitle", Codec: "hdmv_pgs_subtitle"}}}
			p.Format.Name = tc.container
			caps := &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Audio: []string{"aac"}, Remux: true}
			if tc.supported {
				caps.Video = []VideoCapabilityDTO{{Codec: "hevc", Profile: tc.profile, BitDepth: tc.depth, Level: 153, MaxWidth: 3840, MaxHeight: 2160}}
			}
			b := PlaybackRequest{Capabilities: caps, DirectPlay: true, AudioIndex: -1, SubtitleIndex: -1}
			if tc.burn {
				b.SubtitleIndex = 2
			}
			d, err := playbackDecision(p, b, 1000000, 100)
			if err != nil || d.Mode != tc.mode || d.AudioAction != tc.audioAction {
				t.Fatalf("decision %+v %v", d, err)
			}
			if d.VideoAction != "copy" && tc.mode != PlaybackModeTranscode {
				t.Fatalf("unnecessary video encode: %+v", d)
			}
		})
	}
}
func TestPlaybackDecisionRejectsMismatchedProfileAndLimits(t *testing.T) {
	p := media.Probe{Streams: []media.Stream{{Type: "video", Codec: "hevc", Profile: "Main 10", Level: 153, PixelFormat: "yuv420p10le", Width: 3840, Height: 2160}}}
	p.Format.Name = "mp4"
	good := VideoCapabilityDTO{Codec: "hevc", Profile: "Main 10", BitDepth: 10, Level: 153, MaxWidth: 3840, MaxHeight: 2160}
	for _, mutate := range []func(*VideoCapabilityDTO){func(v *VideoCapabilityDTO) { v.Profile = "Main" }, func(v *VideoCapabilityDTO) { v.BitDepth = 8 }, func(v *VideoCapabilityDTO) { v.Level = 120 }, func(v *VideoCapabilityDTO) { v.MaxWidth = 1920 }} {
		v := good
		mutate(&v)
		d, err := playbackDecision(p, PlaybackRequest{AudioIndex: -1, SubtitleIndex: -1, DirectPlay: true, Capabilities: &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Video: []VideoCapabilityDTO{v}}}, 1, 1)
		if err != nil || d.Mode != PlaybackModeTranscode {
			t.Fatalf("mismatch accepted: %+v %v", d, err)
		}
	}
}

func TestPreparedTextSubtitleDoesNotEncodeVideo(t *testing.T) {
	p := media.Probe{Streams: []media.Stream{{Type: "video", Codec: "hevc", Profile: "Main", PixelFormat: "yuv420p", Width: 1920, Height: 1080}, {Type: "subtitle", Index: 2, Codec: "subrip", SubtitleID: "prepared"}}}
	p.Format.Name = "mp4"
	d, err := playbackDecision(p, PlaybackRequest{DirectPlay: true, AudioIndex: -1, SubtitleIndex: 2, Capabilities: &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Video: []VideoCapabilityDTO{{Codec: "hevc", Profile: "Main", BitDepth: 8, MaxWidth: 1920, MaxHeight: 1080}}}}, 1, 1)
	if err != nil || d.Mode != PlaybackModeDirectPlay || d.VideoAction != "copy" || d.SubtitleAction != "external" || d.SubtitleID != "prepared" {
		t.Fatalf("text subtitle forced encoding: %+v %v", d, err)
	}
}

func TestHDRPlaybackDecision(t *testing.T) {
	for _, tc := range []struct {
		name, transfer, container string
		clientHDR, legacy, force  bool
		want                      PlaybackMode
		tone                      bool
	}{
		{"HDR10 native", "smpte2084", "mp4", true, false, false, PlaybackModeDirectPlay, false},
		{"HDR10 remux", "smpte2084", "matroska", true, false, false, PlaybackModeRemux, false},
		{"HDR10 SDR client", "smpte2084", "mp4", false, false, false, PlaybackModeTranscode, true},
		{"HLG SDR client", "arib-std-b67", "matroska", false, false, false, PlaybackModeTranscode, true},
		{"HDR transcode quality/fallback", "smpte2084", "mp4", true, false, true, PlaybackModeTranscode, true},
		{"legacy HDR conversion", "smpte2084", "matroska", false, true, false, PlaybackModeTranscode, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := media.Stream{Type: "video", Codec: "hevc", Profile: "Main 10", PixelFormat: "yuv420p10le", ColorTransfer: tc.transfer, Width: 1920, Height: 1080}
			p := media.Probe{Streams: []media.Stream{v}}
			p.Format.Name = tc.container
			caps := &PlaybackCapabilitiesDTO{Containers: []string{"mp4"}, Remux: true}
			if tc.clientHDR {
				caps.Video = []VideoCapabilityDTO{{Codec: v.Codec, Profile: v.Profile, BitDepth: 10, MaxWidth: 1920, MaxHeight: 1080, HDRFormat: v.HDRFormat()}}
			}
			if tc.legacy {
				caps = nil
			}
			d, err := playbackDecision(p, PlaybackRequest{Capabilities: caps, DirectPlay: !tc.legacy, AudioIndex: -1, SubtitleIndex: -1, ForceTranscode: tc.force}, 1, 1)
			if err != nil || d.Mode != tc.want || d.ToneMapped != tc.tone {
				t.Fatalf("HDR decision %+v %v", d, err)
			}
			if tc.tone && d.VideoCodec != "h264" {
				t.Fatal("tone-mapped output codec not compatible with SDR browser")
			}
		})
	}
}

package server

import (
	"jfe/backend/internal/media"
	"testing"
)

func TestPlaybackMethod(t *testing.T) {
	for _, tc := range []struct {
		name, video, audio, container, pixel, want          string
		direct, force                                       bool
		height, width, limit, bitrate, audioIndex, subtitle int
	}{
		{name: "original MP4", video: "h264", audio: "aac", container: "mp4", direct: true, height: 720, want: "direct"},
		{name: "quality above source", video: "h264", audio: "aac", container: "mp4", direct: true, height: 720, limit: 1080, bitrate: 8000000, want: "direct"},
		{name: "wide source exceeds 4K width", video: "h264", container: "mp4", direct: true, width: 4096, height: 1716, limit: 2160, want: "transcode"},
		{name: "smaller resolution", video: "h264", audio: "aac", container: "mp4", direct: true, height: 1080, limit: 720, want: "transcode"},
		{name: "lower bitrate", video: "h264", audio: "aac", container: "mp4", direct: true, height: 720, bitrate: 100000, want: "transcode"},
		{name: "container remux", video: "h264", audio: "aac", container: "matroska", height: 720, want: "remux"},
		{name: "audio only conversion", video: "h264", audio: "dts", container: "matroska", height: 720, want: "remux"},
		{name: "selected audio", video: "h264", audio: "ac3", container: "mp4", direct: true, height: 720, audioIndex: 1, want: "remux"},
		{name: "browser supports HEVC", video: "hevc", audio: "aac", container: "mp4", direct: true, height: 720, want: "direct"},
		{name: "browser rejects HEVC", video: "hevc", audio: "aac", container: "mp4", height: 720, want: "transcode"},
		{name: "browser supports WebM", video: "vp9", audio: "opus", container: "webm", direct: true, height: 720, want: "direct"},
		{name: "10 bit H264 remux unsupported", video: "h264", pixel: "yuv420p10le", container: "matroska", height: 720, want: "transcode"},
		{name: "decode fallback", video: "h264", container: "mp4", direct: true, force: true, height: 720, want: "transcode"},
		{name: "burn subtitle", video: "h264", container: "mp4", direct: true, height: 720, subtitle: 2, want: "transcode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := media.Probe{Streams: []media.Stream{{Type: "video", Codec: tc.video, Height: tc.height, Width: tc.width, PixelFormat: tc.pixel}, {Type: "audio", Index: 1, Codec: tc.audio}, {Type: "subtitle", Index: 2}}}
			p.Format.Name = tc.container
			b := PlaybackRequest{DirectPlay: tc.direct, ForceTranscode: tc.force, AudioIndex: -1, SubtitleIndex: -1, MaxHeight: tc.limit, MaxBitrate: tc.bitrate}
			if tc.audioIndex != 0 {
				b.AudioIndex = tc.audioIndex
			}
			if tc.subtitle != 0 {
				b.SubtitleIndex = tc.subtitle
			}
			method, err := playbackMethod(p, b, 10000000, 100)
			if err != nil || method != tc.want {
				t.Fatalf("got %s, %v; want %s", method, err, tc.want)
			}
		})
	}
}

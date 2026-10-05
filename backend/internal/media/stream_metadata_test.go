package media

import (
	"encoding/json"
	"testing"
)

func TestProbeDetailedStreamMetadata(t *testing.T) {
	var p Probe
	err := json.Unmarshal([]byte(`{"streams":[{"codec_type":"video","codec_name":"hevc","profile":"Main 10","level":153,"pix_fmt":"yuv420p10le","avg_frame_rate":"24000/1001","color_transfer":"smpte2084","bit_rate":"5000000"},{"codec_type":"audio","codec_name":"truehd","channels":8,"sample_rate":"48000","bit_rate":"2000000"},{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle"}]}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	v := p.Streams[0]
	if v.BitDepth() != 10 || v.HDRFormat() != "hdr10" || v.FPS() < 23.97 || v.FPS() > 23.98 || v.Level != 153 || v.Profile != "Main 10" {
		t.Fatalf("video metadata: %+v", v)
	}
	a := p.Streams[1]
	if a.Channels != 8 || a.SampleRate != "48000" || a.BitRate != "2000000" {
		t.Fatalf("audio metadata: %+v", a)
	}
	if p.Streams[2].SubtitleType() != "bitmap" {
		t.Fatal("PGS type lost")
	}
}

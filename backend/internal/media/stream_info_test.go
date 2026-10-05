package media

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSourceStreamInfo(t *testing.T) {
	var p Probe
	if err := json.Unmarshal([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","bit_rate":"8000000"},{"codec_type":"audio","codec_name":"aac","bit_rate":"192000"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	info := SourceStreamInfo(p, 10500000, 10)
	if info.VideoTranscoded || info.VideoBitrate != 8000000 || info.AudioBitrate != 192000 || info.TotalBitrate != 8400000 {
		t.Fatalf("source rates: %+v", info)
	}
	info = SourceStreamInfo(Probe{}, 0, 0)
	if info.VideoBitrate != 0 || info.AudioBitrate != 0 || info.TotalBitrate != 0 {
		t.Fatalf("missing rates fabricated: %+v", info)
	}
}

func TestInspectSegment(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	// MPEG-2 is used only as a test fixture; production video encoding remains NVENC.
	path := filepath.Join(t.TempDir(), "sample.ts")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=24", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", path}
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, b)
	}
	info, err := InspectSegment(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if info.VideoBitrate <= 0 || info.AudioBitrate <= 0 || info.TotalBitrate <= 0 || info.AudioCodec != "aac" || info.VideoCodec != "mpeg2video" || info.BitrateSource != "segment" {
		t.Fatalf("segment measurements: %+v", info)
	}
}

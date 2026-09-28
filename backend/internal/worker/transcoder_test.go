package worker

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"jfe/backend/internal/media"
)

func TestHLSHasInitialBufferRequiresTwoPublishedSegments(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:4,\nsegment-000000.ts\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segment-000000.ts"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if hlsHasInitialBuffer(dir) {
		t.Fatal("one segment must not mark playback ready")
	}
	if err := os.WriteFile(filepath.Join(dir, "segment-000001.ts"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:4,\nsegment-000000.ts\n#EXTINF:4,\nsegment-000001.ts\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !hlsHasInitialBuffer(dir) {
		t.Fatal("two published segments must mark playback ready")
	}
}

func TestResolutionFilter(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	for _, tc := range []struct {
		name, source                string
		height, width, outputHeight int
	}{
		{"720p", "1920x1080", 720, 1280, 720},
		{"1080p", "3840x2160", 1080, 1920, 1080},
		{"4k", "4096x2304", 2160, 3840, 2160},
		{"no upscale", "640x360", 2160, 640, 360},
		{"portrait", "1080x1920", 720, 405, 720},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.nut")
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=size="+tc.source,
				"-frames:v", "1", "-vf", resolutionFilter(tc.height), "-c:v", "rawvideo", "-pix_fmt", "yuv420p", path)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("encode: %v %s", err, b)
			}
			probe, err := media.Inspect(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if len(probe.Streams) != 1 || math.Abs(float64(probe.Streams[0].Width-tc.width)) > 1 || probe.Streams[0].Width%2 != 0 || probe.Streams[0].Height != tc.outputHeight {
				t.Fatalf("unexpected dimensions: %+v", probe.Streams)
			}
		})
	}
}

func TestRemuxAudioConversion(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	for _, codec := range []string{"aac", "ac3"} {
		t.Run(codec, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source.mka")
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.3", "-c:a", codec, source)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, output)
			}
			probe, err := media.Inspect(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "audio.m4a")
			args := append([]string{"-v", "error", "-i", source}, remuxAudioArgs(probe, -1)...)
			args = append(args, dest)
			if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("remux audio: %v %s", err, output)
			}
			result, err := media.Inspect(context.Background(), dest)
			if err != nil || len(result.Streams) != 1 || result.Streams[0].Codec != "aac" {
				t.Fatalf("audio output: %+v %v", result, err)
			}
		})
	}
}

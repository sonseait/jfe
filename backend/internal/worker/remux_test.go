package worker

import (
	"context"
	"jfe/backend/internal/media"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealFragmentedMP4VideoCopy(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=size=128x72:rate=24", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "5", "-c:v", "libx265", "-x265-params", "log-level=error:pools=1", "-c:a", "aac", source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	args := []string{"-v", "error", "-i", source, "-map", "0:v:0", "-map", "0:a:0", "-c:v", "copy", "-tag:v", "hvc1", "-c:a", "copy"}
	args = append(args, remuxOutputArgs()...)
	cmd = exec.Command("ffmpeg", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remux: %v %s", err, out)
	}
	if !remuxHasInitialBuffer(dir) {
		t.Fatal("complete fMP4 not ready")
	}
	p, err := media.Inspect(context.Background(), filepath.Join(dir, "stream.mp4"))
	if err != nil || len(p.Streams) != 2 || p.Streams[0].Codec != "hevc" || p.Streams[1].Codec != "aac" {
		t.Fatalf("output %+v %v", p, err)
	}
	// Readiness must not publish a partial moov/moof/mdat.
	data, err := os.ReadFile(filepath.Join(dir, "stream.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "stream.mp4"), data[:len(data)/4], 0600); err != nil {
		t.Fatal(err)
	}
	if remuxHasInitialBuffer(dir) {
		t.Fatal("partial fragment published")
	}
}

package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		path, title, series   string
		year, season, episode int
	}{{"/movies/Quiet.Horizon.(2025).1080p.mkv", "Quiet Horizon", "", 2025, 0, 0}, {"/tv/Show.Name.S01E02.mkv", "Show Name S01E02", "Show Name", 0, 1, 2}, {"/tv/One.Piece.S23E1162.mkv", "One Piece S23E1162", "One Piece", 0, 23, 1162}} {
		n := Parse(tc.path)
		if n.Title != tc.title || n.Series != tc.series || n.Year != tc.year || n.Season != tc.season || n.Episode != tc.episode {
			t.Errorf("%s: %+v", tc.path, n)
		}
	}
}
func TestSymlinkContainment(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "private"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := Within(root, filepath.Join(root, "link", "private")); e == nil {
		t.Fatal("escaped media root")
	}
}
func TestProbeScalar(t *testing.T) {
	for _, v := range []string{`"12.5"`, `12.5`} {
		var s Scalar
		if e := json.Unmarshal([]byte(v), &s); e != nil || s != "12.5" {
			t.Fatalf("%s: %s %v", v, s, e)
		}
	}
}

func TestInspectIncludesFFprobeDiagnostic(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ffprobe"), []byte("#!/bin/sh\necho 'Invalid data found when processing input' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	_, err := Inspect(context.Background(), "broken.mkv")
	if err == nil || !strings.Contains(err.Error(), "Invalid data found when processing input") {
		t.Fatalf("missing ffprobe diagnostic: %v", err)
	}
}

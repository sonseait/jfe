package worker

import (
	"strings"
	"testing"
)

func TestBitmapSubtitleGraphUsesSDRBase(t *testing.T) {
	graph := bitmapSubtitleGraph("tone_map", 2, 1.5, []string{resolutionFilter(720)})
	if !strings.HasPrefix(graph, "[0:v:0]tone_map[sdr];") || !strings.Contains(graph, "[sdr][sub]overlay,scale=") || !strings.Contains(graph, "setpts=PTS+1.500/TB") {
		t.Fatalf("HDR subtitle graph: %s", graph)
	}
	if plain := bitmapSubtitleGraph("", 2, 0, nil); strings.Contains(plain, "[sdr]") || !strings.Contains(plain, "[0:v:0][sub]overlay[v]") {
		t.Fatalf("SDR subtitle graph: %s", plain)
	}
}

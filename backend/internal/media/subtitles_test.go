package media

import (
	"strings"
	"testing"
)

func TestParseSubtitles(t *testing.T) {
	for _, content := range []string{
		"\ufeff1\r\n00:00:01,500 --> 00:00:03,000\r\n<b>Hello</b> &amp; welcome\r\nSecond line\r\n",
		"WEBVTT\n\nNOTE ignore 00:00:00.000 --> 00:00:10.000\nnot a cue\n\nfirst\n00:01.500 --> 00:03.000 align:start\n<b>Hello</b> &amp; welcome\nSecond line\n",
	} {
		cues, err := ParseSubtitles(content)
		if err != nil || len(cues) != 1 || cues[0].Start != 1.5 || cues[0].End != 3 || cues[0].Text != "Hello & welcome\nSecond line" {
			t.Fatalf("cues=%+v error=%v", cues, err)
		}
	}
	for _, content := range []string{"", "not subtitles", "00:61:00,000 --> 00:62:00,000\nBad", "00:00:03.000 --> 00:00:01.000\nBad", string([]byte{255}), strings.Repeat("x", 512*1024+1)} {
		if _, err := ParseSubtitles(content); err == nil {
			t.Fatal("invalid subtitles accepted")
		}
	}
}

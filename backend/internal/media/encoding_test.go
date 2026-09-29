package media

import (
	"slices"
	"testing"
)

func TestEncodingModes(t *testing.T) {
	for _, data := range []string{`{}`, `{"threads":2,"crf":23}`, `{"mode":"disabled"}`} {
		cfg, err := ParseEncoding([]byte(data))
		if err != nil || cfg.Mode != "disabled" {
			t.Fatalf("default must disable encoding: %+v %v", cfg, err)
		}
		if _, err := cfg.VideoArgs(cfg.VideoBitrate(1080, 0)); err == nil {
			t.Fatal("disabled mode generated encoding arguments")
		}
	}
	for _, data := range []string{`{"mode":"cpu"}`, `{"mode":"auto"}`, `{"cq":52}`, `{"device":-1}`, `{"maxConcurrent":0}`, `{"videoCodec":"av1"}`, `{"audioBitrate":1}`, `{"subtitleSize":1}`, `{"subtitleColor":"blue"}`} {
		if _, err := ParseEncoding([]byte(data)); err == nil {
			t.Fatalf("accepted invalid configuration %s", data)
		}
	}
	cfg, err := ParseEncoding([]byte(`{"mode":"nvidia","cq":19,"device":2,"maxConcurrent":3}`))
	if err != nil {
		t.Fatal(err)
	}
	args, err := cfg.VideoArgs(cfg.VideoBitrate(1080, 0))
	want := []string{"-c:v", "h264_nvenc", "-gpu", "2", "-preset", "p4", "-rc", "vbr", "-cq", "19", "-b:v", "8000000", "-maxrate", "8000000", "-bufsize", "16000000", "-pix_fmt", "yuv420p"}
	if err != nil || !slices.Equal(args, want) {
		t.Fatalf("NVENC arguments: %v %v", args, err)
	}
	if got := cfg.VideoBitrate(720, 2000000); got != 2000000 {
		t.Fatalf("bitrate limit ignored: %d", got)
	}
	cfg.SubtitleColor, cfg.SubtitleBorderColor, cfg.SubtitleBackground = "#112233", "#445566", "#778899"
	cfg.SubtitleBackgroundOpacity = 50
	if style := cfg.SubtitleStyle(); style != "FontName=Arial,FontSize=24,PrimaryColour=&H00332211,OutlineColour=&H00665544,BackColour=&H80998877,Outline=2,MarginV=32,BorderStyle=3" {
		t.Fatalf("subtitle style: %s", style)
	}
}

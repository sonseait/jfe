package media

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestToneMappingMetadataAndSafety(t *testing.T) {
	if filter, err := ToneMappingFilter(Stream{ColorTransfer: "bt709"}); err != nil || filter != "" {
		t.Fatal("SDR was tone mapped")
	}
	for _, transfer := range []string{"smpte2084", "arib-std-b67"} {
		filter, err := ToneMappingFilter(Stream{ColorTransfer: transfer})
		if err != nil || !strings.Contains(filter, "transferin="+transfer) {
			t.Fatalf("HDR filter: %s %v", filter, err)
		}
	}
	var hdr10plus Stream
	if err := json.Unmarshal([]byte(`{"color_transfer":"smpte2084","side_data_list":[{"side_data_type":"HDR Dynamic Metadata SMPTE2094-40"}]}`), &hdr10plus); err != nil {
		t.Fatal(err)
	}
	filter, err := ToneMappingFilter(hdr10plus)
	if err != nil || hdr10plus.HDRFormat() != "hdr10plus" || !strings.Contains(filter, "transferin=smpte2084") || !strings.Contains(filter, "type=DYNAMIC_HDR_PLUS") {
		t.Fatalf("HDR10+ static base mapping: %s %v", filter, err)
	}
	var dv Stream
	_ = json.Unmarshal([]byte(`{"color_transfer":"smpte2084","side_data_list":[{"side_data_type":"HDR Dynamic Metadata SMPTE2094-40"},{"side_data_type":"DOVI configuration record"}]}`), &dv)
	if dv.HDRFormat() != "dolby_vision" {
		t.Fatal("Dolby Vision misidentified as HDR10+")
	}
	if _, err := ToneMappingFilter(dv); err == nil {
		t.Fatal("Dolby Vision processed without RPU support")
	}
	if _, err := ToneMappingFilter(Stream{ColorTransfer: "smpte2084", ColorSpace: "bt2020nc,evil"}); err == nil {
		t.Fatal("untrusted filter value accepted")
	}
}

// These software encoders create/inspect fixtures only. Playback retains NVENC.
func TestRealHDRToneMapping(t *testing.T) {
	filters, err := exec.Command("ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil || !strings.Contains(string(filters), " zscale ") {
		t.Skip("requires FFmpeg with libzimg/zscale; production Debian image includes it")
	}
	for _, transfer := range []string{"smpte2084", "arib-std-b67"} {
		t.Run(transfer, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "hdr.mkv")
			out := filepath.Join(dir, "sdr.mp4")
			run := func(args ...string) []byte {
				t.Helper()
				data, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("FFmpeg: %v %s", err, data)
				}
				return data
			}
			run("-f", "lavfi", "-i", "nullsrc=size=96x64:rate=2,format=yuv420p10le,geq=lum='if(lt(X,W/3),128,if(lt(X,2*W/3),512,850))':cb=512:cr=512", "-t", "1", "-c:v", "libx265", "-x265-params", "pools=1:log-level=error:master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400", "-color_primaries", "bt2020", "-color_trc", transfer, "-colorspace", "bt2020nc", "-color_range", "tv", source)
			probe, err := Inspect(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			filter, err := ToneMappingFilter(probe.Streams[0])
			if err != nil || filter == "" {
				t.Fatalf("source %+v %v", probe, err)
			}
			args := []string{"-i", source, "-vf", filter, "-c:v", "libx264"}
			args = append(args, SDRColorArgs()...)
			args = append(args, out)
			run(args...)
			converted, err := Inspect(context.Background(), out)
			if err != nil {
				t.Fatal(err)
			}
			s := converted.Streams[0]
			if s.ColorTransfer != "bt709" || s.ColorPrimaries != "bt709" || s.ColorSpace != "bt709" || s.ColorRange != "tv" || s.BitDepth() != 8 || s.HDRFormat() != "" {
				t.Fatalf("not BT.709 SDR: %+v", s)
			}
			raw := run("-i", out, "-frames:v", "1", "-pix_fmt", "yuv420p", "-f", "rawvideo", "pipe:1")
			unchanged := run("-i", source, "-vf", "format=yuv420p", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
			if len(raw) != 96*64*3/2 {
				t.Fatalf("frame size %d", len(raw))
			}
			dark, mid, bright := raw[16], raw[48], raw[80]
			if dark < 16 || bright > 235 || !(dark < mid && mid < bright) {
				t.Fatalf("clipped/non-monotonic luminance: %d %d %d", dark, mid, bright)
			}
			if mid == unchanged[48] {
				t.Fatal("HDR pixels were only retagged, not mapped")
			}
			if chroma := raw[96*64]; chroma < 126 || chroma > 130 {
				t.Fatalf("neutral chroma shifted: %d", chroma)
			}
			frames, err := exec.Command("ffprobe", "-v", "error", "-read_intervals", "%+#1", "-show_frames", "-of", "json", out).Output()
			if err != nil {
				t.Fatal(err)
			}
			for _, hdrMetadata := range []string{"Mastering display metadata", "Content light level metadata", "HDR Dynamic Metadata"} {
				if strings.Contains(string(frames), hdrMetadata) {
					t.Fatalf("HDR frame metadata leaked: %s", hdrMetadata)
				}
			}
		})
	}
}

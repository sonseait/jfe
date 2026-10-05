package worker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func requireToneMappingFilters(ctx context.Context) error {
	data, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return fmt.Errorf("inspect FFmpeg tone mapping filters: %w", err)
	}
	available := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			available[fields[1]] = true
		}
	}
	for _, filter := range []string{"zscale", "tonemap", "sidedata", "limiter"} {
		if !available[filter] {
			return fmt.Errorf("HDR tone mapping requires FFmpeg filter %s; install FFmpeg with libzimg", filter)
		}
	}
	return nil
}

// Composite SDR subtitles after tone mapping and before the requested resize.
func bitmapSubtitleGraph(toneMap string, index int, delay float64, filters []string) string {
	input, prefix := "[0:v:0]", ""
	if toneMap != "" {
		prefix = "[0:v:0]" + toneMap + "[sdr];"
		input = "[sdr]"
	}
	return prefix + fmt.Sprintf("[0:%d]setpts=PTS%+.3f/TB[sub];%s[sub]overlay%s[v]", index, delay, input, filterSuffix(filters))
}

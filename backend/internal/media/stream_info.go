package media

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"
)

// StreamInfo describes media bitrate, not the speed of downloading media.
type StreamInfo struct {
	ToneMapped      bool   `json:"toneMapped,omitempty"`
	VideoTranscoded bool   `json:"videoTranscoded"`
	VideoCodec      string `json:"videoCodec"`
	AudioCodec      string `json:"audioCodec"`
	VideoBitrate    int64  `json:"videoBitrate"`
	AudioBitrate    int64  `json:"audioBitrate"`
	TotalBitrate    int64  `json:"totalBitrate"`
	BitrateSource   string `json:"bitrateSource"`
}

func SourceStreamInfo(probe Probe, size int64, duration float64) StreamInfo {
	info := StreamInfo{BitrateSource: "metadata"}
	for _, track := range probe.Streams {
		rate, _ := strconv.ParseInt(string(track.BitRate), 10, 64)
		if rate <= 0 {
			for _, key := range []string{"BPS", "BPS-eng"} {
				if tagged, err := strconv.ParseInt(track.Tags[key], 10, 64); err == nil && tagged > 0 {
					rate = tagged
					break
				}
			}
		}
		if rate < 0 {
			rate = 0
		}
		if track.Type == "video" && track.Disposition.AttachedPic == 0 && info.VideoCodec == "" {
			info.VideoCodec, info.VideoBitrate = track.Codec, rate
		}
		if track.Type == "audio" && info.AudioCodec == "" {
			info.AudioCodec, info.AudioBitrate = track.Codec, rate
		}
	}
	if size > 0 && duration > 0 {
		info.TotalBitrate = int64(float64(size) * 8 / duration)
	}
	return info
}

// Sample a bounded interval of an HLS segment or growing remux output. Packet sizes separate video/audio payload;
// format bitrate includes transport overhead. This is a sample, not a target rate.
func InspectSegment(ctx context.Context, path string) (StreamInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-read_intervals", "%+4", "-show_entries",
		"stream=index,codec_type,codec_name:format=bit_rate:packet=stream_index,size,pts_time,duration_time",
		"-of", "json", path).Output()
	if err != nil {
		return StreamInfo{}, err
	}
	var data struct {
		Streams []Stream `json:"streams"`
		Format  struct {
			BitRate Scalar `json:"bit_rate"`
		} `json:"format"`
		Packets []struct {
			StreamIndex int    `json:"stream_index"`
			Size        Scalar `json:"size"`
			PTS         Scalar `json:"pts_time"`
			Duration    Scalar `json:"duration_time"`
		} `json:"packets"`
	}
	if err = json.Unmarshal(output, &data); err != nil {
		return StreamInfo{}, err
	}
	info := SourceStreamInfo(Probe{Streams: data.Streams}, 0, 0)
	info.BitrateSource = "segment"
	info.TotalBitrate, _ = strconv.ParseInt(string(data.Format.BitRate), 10, 64)
	for _, stream := range data.Streams {
		var bytes int64
		var start, end float64
		found := false
		for _, packet := range data.Packets {
			if packet.StreamIndex != stream.Index {
				continue
			}
			pts, e := strconv.ParseFloat(string(packet.PTS), 64)
			if e != nil {
				continue
			}
			size, _ := strconv.ParseInt(string(packet.Size), 10, 64)
			duration, _ := strconv.ParseFloat(string(packet.Duration), 64)
			if !found || pts < start {
				start = pts
			}
			if !found || pts+duration > end {
				end = pts + duration
			}
			bytes += size
			found = true
		}
		if found && end > start {
			rate := int64(float64(bytes) * 8 / (end - start))
			if stream.Type == "video" {
				info.VideoBitrate = rate
			}
			if stream.Type == "audio" {
				info.AudioBitrate = rate
			}
		}
	}
	return info, nil
}

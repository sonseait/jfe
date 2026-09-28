package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errPlaybackStopped = errors.New("playback stopped")

// An EVENT playlist needs media ahead of the first segment so the player does not stall at its edge.
func hlsHasInitialBuffer(dir string) bool {
	playlist, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		return false
	}
	segments := 0
	for _, line := range strings.Split(string(playlist), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, line)); err == nil {
			segments++
		}
	}
	return segments >= 2
}

func (w *Worker) transcode(ctx context.Context, j store.Job) (result error) {
	p, e := w.DB.GetPlayback(ctx, j.ResourceID)
	if e != nil {
		return e
	}
	if p.State != "preparing" {
		if p.State == "stopped" {
			return errPlaybackStopped
		}
		return fmt.Errorf("playback not preparing")
	}
	defer func() {
		if result != nil {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if errors.Is(result, errPlaybackStopped) {
				_ = w.DB.StopPlayback(c, store.StopPlaybackParams{ID: p.ID, UserID: p.UserID})
			} else {
				_ = w.DB.FailPlayback(c, p.ID)
			}
		}
	}()
	f, e := w.DB.GetFile(ctx, p.FileID)
	if e != nil {
		return e
	}
	path, e := audio.Within(w.Config.MediaRoot, w.Config.ImportRoot, f.Path)
	if e != nil {
		return e
	}
	var b struct {
		AudioIndex, SubtitleIndex, MaxBitrate, MaxHeight int
		SubtitleDelay                                    float64
		Position                                         float64
	}
	if e = json.Unmarshal(j.Payload, &b); e != nil {
		return e
	}
	data, e := w.DB.GetSetting(ctx, "encoding")
	if e != nil {
		return e
	}
	cfg, e := media.ParseEncoding(data)
	if e != nil {
		return e
	}
	var videoArgs []string
	if p.Method == "transcode" {
		videoArgs, e = cfg.VideoArgs()
		if e != nil {
			return e
		}
	}
	dir := filepath.Join(w.Config.CacheRoot, "playback", p.ID)
	dir, e = filepath.Abs(dir)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0750); e != nil {
		return e
	}
	defer func() {
		if result != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.3f", p.StartPosition), "-i", path, "-map", "0:v:0"}
	audio := "0:a:0?"
	if b.AudioIndex >= 0 {
		audio = "0:" + strconv.Itoa(b.AudioIndex)
	}
	args = append(args, "-map", audio)
	if p.Method == "audio" {
		args = []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.3f", p.StartPosition), "-i", path, "-map", audio, "-vn", "-c:a", "aac", "-b:a", "192k", "-ac", "2"}
	} else if p.Method == "remux" {
		var probe media.Probe
		if e = json.Unmarshal(f.Probe, &probe); e != nil {
			return e
		}
		args = append(args, "-c:v", "copy")
		args = append(args, remuxAudioArgs(probe, b.AudioIndex)...)
	} else {
		args = append(args, videoArgs...)
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
		if b.MaxBitrate > 0 {
			args = append(args, "-maxrate", strconv.Itoa(b.MaxBitrate), "-bufsize", strconv.Itoa(b.MaxBitrate*2))
		}
		filters := []string{}
		if b.MaxHeight > 0 {
			filters = append(filters, resolutionFilter(b.MaxHeight))
		}
		if b.SubtitleIndex >= 0 {
			var probe media.Probe
			_ = json.Unmarshal(f.Probe, &probe)
			bitmap := false
			for _, s := range probe.Streams {
				if s.Index == b.SubtitleIndex && (s.Codec == "hdmv_pgs_subtitle" || s.Codec == "dvd_subtitle") {
					bitmap = true
				}
			}
			if bitmap {
				args = append(args, "-filter_complex", fmt.Sprintf("[0:%d]setpts=PTS%+.3f/TB[sub];[0:v:0][sub]overlay%s[v]", b.SubtitleIndex, b.SubtitleDelay, filterSuffix(filters)))
				for n := 0; n < len(args)-1; n++ {
					if args[n] == "-map" && args[n+1] == "0:v:0" {
						args[n+1] = "[v]"
					}
				}
				filters = nil
			} else {
				subtitlePath, subtitleMap := path, "0:"+strconv.Itoa(b.SubtitleIndex)
				for _, track := range probe.Streams {
					if track.Index == b.SubtitleIndex && track.ExternalPath != "" {
						subtitlePath, e = media.Within(w.Config.MediaRoot, track.ExternalPath)
						if e != nil {
							return e
						}
						subtitleMap = "0:0"
					}
				}
				extract := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", subtitlePath, "-map", subtitleMap, "-c:s", "ass", filepath.Join(dir, "subtitles.ass"))
				if e = extract.Run(); e != nil {
					return fmt.Errorf("subtitle extraction failed: %w", e)
				}
				filters = append(filters, fmt.Sprintf("setpts=PTS%+.3f/TB,subtitles=subtitles.ass,setpts=PTS-STARTPTS", p.StartPosition-b.SubtitleDelay))
			}
		}
		if len(filters) > 0 {
			args = append(args, "-vf", strings.Join(filters, ","))
		}
		args = append(args, "-force_key_frames", "expr:gte(t,n_forced*4)")
	}
	args = append(args, "-sn", "-f", "hls", "-hls_time", "4", "-hls_playlist_type", "event", "-hls_list_size", "0", "-hls_flags", "independent_segments+temp_file", "-hls_segment_filename", "segment-%06d.ts", "index.m3u8")
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(processCtx, "ffmpeg", args...)
	cmd.Dir = dir
	logFile, e := os.OpenFile(filepath.Join(dir, "ffmpeg.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer logFile.Close()
	cmd.Stderr = logFile
	if e = cmd.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	measurementCtx, stopMeasurement := context.WithCancel(ctx)
	var measured chan struct{}
	defer func() {
		defer stopMeasurement()
		if result != nil {
			stopMeasurement()
		}
		if measured != nil {
			// A stopped session must release the worker even when FFmpeg finished before ffprobe.
			for {
				select {
				case <-measured:
					return
				case <-ctx.Done():
					stopMeasurement()
					<-measured
					return
				case <-ticker.C:
					current, err := w.DB.GetPlayback(ctx, p.ID)
					if err != nil || current.State == "stopped" || current.State == "failed" || current.ExpiresAt.Before(time.Now()) {
						if result == nil && err == nil && (current.State == "stopped" || current.ExpiresAt.Before(time.Now())) {
							result = errPlaybackStopped
						}
						stopMeasurement()
						<-measured
						return
					}
				}
			}
		}
	}()
	markReady := func() error {
		info := media.StreamInfo{VideoTranscoded: p.Method == "transcode", BitrateSource: "pending"}
		if err := writeStreamInfo(dir, info); err != nil {
			return err
		}
		if err := w.DB.SetPlaybackReady(ctx, p.ID); err != nil {
			return err
		}
		// Playback can begin as soon as the playlist exists; measurements are optional.
		measured = make(chan struct{})
		go func() {
			defer close(measured)
			sample, err := media.InspectSegment(measurementCtx, filepath.Join(dir, "segment-000000.ts"))
			if measurementCtx.Err() != nil {
				return
			}
			if err != nil {
				log.Warn().Err(err).Str("playbackId", p.ID).Msg("Could not measure output stream bitrate")
				sample.BitrateSource = "unavailable"
			}
			sample.VideoTranscoded = info.VideoTranscoded
			if err := writeStreamInfo(dir, sample); err != nil {
				log.Warn().Err(err).Str("playbackId", p.ID).Msg("Could not save output stream bitrate")
			}
		}()
		return nil
	}
	for {
		select {
		case err := <-done:
			if err != nil {
				// Capture the bounded diagnostic before failed-session cleanup removes the log.
				info, statErr := logFile.Stat()
				diagnostic := make([]byte, 4096)
				n := 0
				if statErr == nil {
					reader, openErr := os.Open(logFile.Name())
					if openErr == nil {
						n, _ = reader.ReadAt(diagnostic, max(0, info.Size()-4096))
						reader.Close()
					}
				}
				return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(string(diagnostic[:n])))
			}
			if !ready {
				if e = markReady(); e != nil {
					return e
				}
			}
			return nil
		case <-ticker.C:
			current, e := w.DB.GetPlayback(ctx, p.ID)
			if e != nil || current.State == "stopped" || current.State == "failed" || current.ExpiresAt.Before(time.Now()) {
				cancel()
				<-done
				if e != nil {
					return fmt.Errorf("read playback state: %w", e)
				}
				if current.State == "failed" {
					return errors.New("playback session failed")
				}
				return errPlaybackStopped
			}
			if !ready {
				if hlsHasInitialBuffer(dir) {
					if e = markReady(); e != nil {
						cancel()
						<-done
						return e
					}
					ready = true
				}
			}
		case <-ctx.Done():
			cancel()
			<-done
			return ctx.Err()
		}
	}
}

func writeStreamInfo(dir string, info media.StreamInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	temp := filepath.Join(dir, "stream-info.json.tmp")
	if err = os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(dir, "stream-info.json"))
}

func remuxAudioArgs(probe media.Probe, index int) []string {
	codec := ""
	for _, track := range probe.Streams {
		if track.Type == "audio" && (index == -1 || track.Index == index) {
			codec = track.Codec
			break
		}
	}
	if codec == "" || codec == "aac" {
		return []string{"-c:a", "copy"}
	}
	return []string{"-c:a", "aac", "-ac", "2", "-b:a", "192k"}
}
func (w *Worker) cleanup(ctx context.Context) {
	root := filepath.Join(w.Config.CacheRoot, "playback")
	entries, e := os.ReadDir(root)
	if e != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p, e := w.DB.GetPlayback(ctx, entry.Name())
		if e == nil && (p.State == "ready" || p.State == "preparing") {
			continue
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}

// Fit inside the chosen resolution without upscaling; H.264 requires even dimensions.
func resolutionFilter(height int) string {
	width := height * 16 / 9
	return fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:force_divisible_by=2", width, height)
}
func filterSuffix(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	return "," + strings.Join(filters, ",")
}

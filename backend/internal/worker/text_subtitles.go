package worker

import (
	"context"
	"encoding/json"
	"io"
	"jfe/backend/internal/media"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Scanner publishes plain-text cues for editor previews and FFmpeg text burn-in
// without rereading unchanged movies. ASS retains its source styling.
func (w *Worker) prepareTextSubtitles(ctx context.Context, fileID, source string, probe *media.Probe, unchanged bool) {
	for i := range probe.Streams {
		track := &probe.Streams[i]
		switch track.Codec {
		case "subrip", "srt", "webvtt", "vtt":
		default:
			track.SubtitleID = ""
			continue
		}
		id := stable("subtitle", fileID, strconv.Itoa(track.Index))
		dir := filepath.Join(w.Config.CacheRoot, "subtitles")
		// An unchanged container has the same embedded track content. External
		// sidecars are rediscovered separately and may change without the video.
		if unchanged && track.ExternalPath == "" && track.SubtitleID == id {
			cache := filepath.Join(dir, id+".json")
			if stat, err := os.Stat(cache); err == nil && stat.Mode().IsRegular() && stat.Size() <= 524288 {
				data, err := os.ReadFile(cache)
				var cues []media.Cue
				if err == nil && json.Unmarshal(data, &cues) == nil && cues != nil {
					continue
				}
			}
		}
		track.SubtitleID = ""
		if os.MkdirAll(dir, 0750) != nil {
			continue
		}
		temp, err := os.CreateTemp(dir, "extract-*.vtt")
		if err != nil {
			continue
		}
		temp.Close()
		func() {
			defer os.Remove(temp.Name())
			input, selection := source, "0:"+strconv.Itoa(track.Index)
			if track.ExternalPath != "" {
				input, err = media.Within(w.Config.MediaRoot, track.ExternalPath)
				if err != nil {
					return
				}
				selection = "0:0"
			}
			runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(runCtx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", input, "-map", selection, "-c:s", "webvtt", "-fs", "524289", temp.Name())
			if cmd.Run() != nil {
				return
			}
			f, err := os.Open(temp.Name())
			if err != nil {
				return
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, 524289))
			if err != nil {
				return
			}
			cues, err := media.ParseSubtitles(string(data))
			if err != nil {
				return
			}
			encoded, err := json.Marshal(cues)
			if err != nil {
				return
			}
			output, err := os.CreateTemp(dir, "cues-*.json")
			if err != nil {
				return
			}
			defer os.Remove(output.Name())
			if _, err = output.Write(encoded); err != nil {
				output.Close()
				return
			}
			if output.Close() != nil {
				return
			}
			if os.Rename(output.Name(), filepath.Join(dir, id+".json")) == nil {
				track.SubtitleID = id
			}
		}()
	}
}

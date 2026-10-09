package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const subtitleCacheVersion = 1
const subtitleCacheLimit = 512 * 1024

type subtitleCacheRecord struct {
	Fingerprint string    `json:"fingerprint"`
	RetryAfter  time.Time `json:"retryAfter,omitempty"`
}

// Scanner publishes plain-text cues for editor previews and FFmpeg text burn-in.
// Large containers use a metadata fingerprint; small sidecars use a content hash.
func (w *Worker) prepareTextSubtitles(ctx context.Context, fileID, source string, probe *media.Probe, unchanged bool, deferEmbedded ...bool) bool {
	pending := false
	dir := filepath.Join(w.Config.CacheRoot, "subtitles")
	type extraction struct {
		path, selection string
		publish         func([]byte) error
		failed          func()
	}
	var extracts []extraction
	for i := range probe.Streams {
		if ctx.Err() != nil {
			return pending
		}
		track := &probe.Streams[i]
		switch track.Codec {
		case "subrip", "srt", "webvtt", "vtt":
		default:
			track.SubtitleID = ""
			continue
		}
		id := stable("subtitle", fileID, strconv.Itoa(track.Index))
		previousID := track.SubtitleID
		track.SubtitleID = ""
		input, selection := source, "0:"+strconv.Itoa(track.Index)
		var content []byte
		var err error
		if track.ExternalPath != "" {
			input, err = media.Within(w.Config.MediaRoot, track.ExternalPath)
			if err != nil {
				continue
			}
			selection = "0:0"
		}
		stat, err := os.Stat(input)
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}
		if track.ExternalPath != "" {
			content, err = readSubtitleCacheInput(input)
			if err != nil {
				continue
			}
		}
		identity := fmt.Sprintf("%d\x00%d\x00%s\x00%d\x00%d\x00%s\x00%s", subtitleCacheVersion, media.ProbeVersion, input, stat.Size(), stat.ModTime().UnixNano(), selection, track.Codec)
		if track.ExternalPath != "" {
			// Content hashing is cheap for bounded SRT/VTT and catches edits even
			// when a tool preserves file size and modification time.
			identity = fmt.Sprintf("%d\x00%s\x00%s\x00%s", subtitleCacheVersion, input, track.Codec, content)
		}
		digest := sha256.Sum256([]byte(identity))
		fingerprint := hex.EncodeToString(digest[:])
		cache := filepath.Join(dir, id+".json")
		manifest := filepath.Join(dir, id+".fingerprint.json")
		var record subtitleCacheRecord
		manifestData, manifestErr := os.ReadFile(manifest)
		if manifestErr == nil && json.Unmarshal(manifestData, &record) == nil && record.Fingerprint == fingerprint {
			if record.RetryAfter.After(time.Now()) {
				continue
			}
			if record.RetryAfter.IsZero() && validSubtitleCache(cache) {
				track.SubtitleID = id
				continue
			}
		} else if os.IsNotExist(manifestErr) && unchanged && track.ExternalPath == "" && previousID == id && validSubtitleCache(cache) {
			// Adopt existing scanner caches without rereading unchanged movies.
			if os.MkdirAll(dir, 0750) == nil {
				_ = writeSubtitleCacheRecord(manifest, subtitleCacheRecord{Fingerprint: fingerprint})
			}
			track.SubtitleID = id
			continue
		}
		if track.ExternalPath == "" && len(deferEmbedded) > 0 && deferEmbedded[0] {
			pending = true
			continue
		}
		if os.MkdirAll(dir, 0750) != nil {
			continue
		}

		failed := func() {
			if ctx.Err() == nil {
				_ = writeSubtitleCacheRecord(manifest, subtitleCacheRecord{Fingerprint: fingerprint, RetryAfter: time.Now().Add(10 * time.Minute)})
			}
		}
		publish := func(content []byte) error {
			cues, err := media.ParseSubtitles(string(content))
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(cues)
			if err != nil {
				return err
			}
			if len(encoded) > subtitleCacheLimit {
				return fmt.Errorf("subtitle exceeds cache limit")
			}
			current, err := os.Stat(input)
			if err != nil {
				return err
			}
			if current.Size() != stat.Size() || !current.ModTime().Equal(stat.ModTime()) {
				return fmt.Errorf("subtitle source changed")
			}
			if err = writeSubtitleCacheFile(cache, encoded); err != nil {
				return err
			}
			if err = writeSubtitleCacheRecord(manifest, subtitleCacheRecord{Fingerprint: fingerprint}); err != nil {
				return err
			}
			track.SubtitleID = id
			return nil
		}
		if track.ExternalPath != "" {
			if publish(content) != nil {
				failed()
			}
			continue
		}
		temp, err := os.CreateTemp(dir, "extract-*.vtt")
		if err != nil {
			failed()
			continue
		}
		temp.Close()
		defer os.Remove(temp.Name())
		extracts = append(extracts, extraction{temp.Name(), selection, publish, failed})
	}
	if len(extracts) > 0 && ctx.Err() == nil {
		// One demux pass for every missing text track, instead of rereading a
		// multi-gigabyte container once per language. Never encode video/audio.
		args := []string{"-nostdin", "-v", "error", "-y", "-i", source}
		for _, extract := range extracts {
			args = append(args, "-map", extract.selection, "-c:s", "webvtt", "-fs", "524289", extract.path)
		}
		runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err := exec.CommandContext(runCtx, "ffmpeg", args...).Run()
		cancel()
		for _, extract := range extracts {
			if err != nil {
				extract.failed()
				continue
			}
			content, readErr := readSubtitleCacheInput(extract.path)
			if readErr != nil || extract.publish(content) != nil {
				extract.failed()
			}
		}
	}

	return pending
}

func readSubtitleCacheInput(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, subtitleCacheLimit+1))
	if err == nil && len(data) > subtitleCacheLimit {
		return nil, fmt.Errorf("subtitle exceeds cache size limit")
	}
	return data, err
}

func validSubtitleCache(path string) bool {
	data, err := readSubtitleCacheInput(path)
	var cues []media.Cue
	if err != nil || json.Unmarshal(data, &cues) != nil {
		return false
	}
	_, err = media.SubtitlesASS(cues)
	return err == nil
}

func writeSubtitleCacheRecord(path string, record subtitleCacheRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return writeSubtitleCacheFile(path, data)
}

func writeSubtitleCacheFile(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "subtitle-cache-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// Embedded extraction runs under the same per-file lock as scans and source edits.
func (w *Worker) prepareSubtitleJob(ctx context.Context, job store.Job) error {
	file, err := w.DB.GetFile(ctx, job.ResourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	path, err := media.Within(w.Config.MediaRoot, file.Path)
	if err != nil {
		return err
	}
	unlock, err := audio.Lock(ctx, w.Config.CacheRoot, path)
	if err != nil {
		return err
	}
	defer unlock()
	file, err = w.DB.GetFile(ctx, job.ResourceID)
	if err != nil {
		return err
	}
	if !file.Available {
		return nil
	}
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.Size() != file.Size || stat.ModTime().UnixNano() != file.ModifiedAt {
		return nil
	}
	var probe media.Probe
	if err = json.Unmarshal(file.Probe, &probe); err != nil {
		return err
	}
	w.prepareTextSubtitles(ctx, file.ID, path, &probe, true)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	if current.Size() != file.Size || current.ModTime().UnixNano() != file.ModifiedAt {
		return nil
	}
	data, err := json.Marshal(probe)
	if err != nil {
		return err
	}
	return w.DB.UpdateSubtitleFileProbe(ctx, store.UpdateSubtitleFileProbeParams{ID: file.ID, Probe: data, Size: file.Size, ModifiedAt: file.ModifiedAt})
}

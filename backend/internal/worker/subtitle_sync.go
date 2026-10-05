package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
)

func subtitleError(code string) error { return errors.New(code) }
func (w *Worker) subtitlePermission(ctx context.Context, user, library string) error {
	if !w.Config.SubtitleEditing {
		return subtitleError("disabled")
	}
	ok, e := w.DB.CanEditSubtitles(ctx, store.CanEditSubtitlesParams{LibraryID: library, UserID: user})
	if e != nil {
		return e
	}
	if !ok {
		return subtitleError("permission")
	}
	return nil
}
func subtitleWritable(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0222 == 0 {
		return subtitleError("read_only")
	}
	if st.Sys().(*syscall.Stat_t).Nlink != 1 {
		return subtitleError("hardlinked")
	}
	f, e := os.OpenFile(path, os.O_WRONLY, 0)
	if e != nil {
		return subtitleError("read_only")
	}
	f.Close()
	t, e := os.CreateTemp(filepath.Dir(path), ".jfe-write-check-")
	if e != nil {
		return subtitleError("read_only")
	}
	t.Close()
	return os.Remove(t.Name())
}
func (w *Worker) subtitleJob(ctx context.Context, j store.Job) (err error) {
	record, e := w.DB.GetSubtitleSyncJob(ctx, j.ID)
	if e != nil {
		return e
	}
	defer func() {
		if err != nil {
			code := strings.SplitN(err.Error(), ":", 2)[0]
			switch code {
			case "disabled", "permission", "read_only", "hardlinked", "subtitle_changed", "subtitle_negative_time", "subtitle_unsupported_format", "subtitle_playback_busy", "subtitle_verification_failed":
			default:
				code = "subtitle_job_failed"
			}
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = w.DB.SetSubtitleSyncPhase(c, store.SetSubtitleSyncPhaseParams{ID: j.ID, Phase: record.Phase, ErrorCode: code})
		}
	}()
	if record.Phase == "indexed" {
		return nil
	}
	f, e := w.DB.GetFile(ctx, record.FileID)
	if e != nil {
		return e
	}
	item, e := w.DB.GetItem(ctx, f.ItemID)
	if e != nil {
		return e
	}
	if e = w.subtitlePermission(ctx, record.UserID, item.LibraryID); e != nil {
		return e
	}
	video, e := media.Within(w.Config.MediaRoot, f.Path)
	if e != nil {
		return e
	}
	// Scanner scans and subtitle operations share this lock, including sidecars.
	unlock, e := audio.Lock(ctx, w.Config.CacheRoot, video)
	if e != nil {
		return e
	}
	defer unlock()
	f, e = w.DB.GetFile(ctx, record.FileID)
	if e != nil {
		return e
	}
	if record.Action == "prepare" && record.ProbeRevision != media.SubtitleProbeRevision(f.Probe) {
		return subtitleError("subtitle_changed")
	}
	var probe media.Probe
	if e = json.Unmarshal(f.Probe, &probe); e != nil {
		return e
	}
	var track *media.Stream
	for n := range probe.Streams {
		if probe.Streams[n].Type == "subtitle" && probe.Streams[n].Index == int(record.TrackIndex) {
			track = &probe.Streams[n]
			break
		}
	}
	if record.Action == "prepare" {
		if track == nil {
			return subtitleError("subtitle_changed")
		}
		source := video
		if track.ExternalPath != "" {
			source, e = media.Within(w.Config.MediaRoot, track.ExternalPath)
			if e != nil {
				return e
			}
		}
		if source != video {
			if !media.SubtitleSidecarSource(video, source) {
				return media.ErrSubtitleFormat
			}
			unlockSource, e := audio.Lock(ctx, w.Config.CacheRoot, source)
			if e != nil {
				return e
			}
			defer unlockSource()
		}
		if e = subtitleWritable(source); e != nil {
			return e
		}
		fingerprint, e := audio.Fingerprint(source)
		if e != nil {
			return e
		}
		cues := []media.Cue{}
		warning := ""
		if media.SubtitleTextCodec(track.Codec) {
			cues, e = w.subtitleCues(ctx, source, *track)
			if e != nil {
				cues = []media.Cue{}
				warning = "subtitle_unsupported_format"
			}
		}
		check, e := audio.Fingerprint(source)
		if e != nil {
			return e
		}
		if check != fingerprint {
			return subtitleError("subtitle_changed")
		}
		data, _ := json.Marshal(cues)
		if e = w.validLease(ctx, j); e != nil {
			return e
		}
		if e = w.subtitlePermission(ctx, record.UserID, item.LibraryID); e != nil {
			return e
		}
		if e = w.DB.PrepareSubtitleSyncJob(ctx, store.PrepareSubtitleSyncJobParams{ID: j.ID, Fingerprint: fingerprint, SourcePath: source, Cues: data}); e != nil {
			return e
		}
		return w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: j.ID, Phase: "ready", ErrorCode: warning})
	}
	busy, e := w.DB.SubtitlePlaybackBusy(ctx, record.FileID)
	if e != nil {
		return e
	}
	if busy {
		return subtitleError("subtitle_playback_busy")
	}
	busy, e = w.DB.SubtitleTranscoderBusy(ctx, record.FileID)
	if e != nil {
		return e
	}
	if busy {
		return subtitleError("subtitle_playback_busy")
	}
	source := record.SourcePath
	// Source identity is written only by scanner during prepare, never supplied by client.
	if source != video {
		if !media.SubtitleSidecarSource(video, source) {
			return subtitleError("subtitle_changed")
		}
		unlockSource, e := audio.Lock(ctx, w.Config.CacheRoot, source)
		if e != nil {
			return e
		}
		defer unlockSource()
	}
	if filepath.Dir(source) != filepath.Dir(video) {
		return subtitleError("subtitle_changed")
	}
	journal := filepath.Join(filepath.Dir(source), ".jfe-subtitle-"+j.ID+".json")
	tmp := filepath.Join(filepath.Dir(source), ".jfe-subtitle-"+j.ID+filepath.Ext(source))
	tomb := filepath.Join(filepath.Dir(source), ".jfe-subtitle-"+j.ID+".old")
	var publication struct {
		Target string `json:"target"`
		Delete bool   `json:"delete"`
	}
	data, readErr := os.ReadFile(journal)
	if readErr == nil {
		if e = json.Unmarshal(data, &publication); e != nil {
			return e
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	current, e := audio.Fingerprint(source)
	deleted := record.Action == "delete" && source != video
	published := readErr == nil && ((deleted && errors.Is(e, os.ErrNotExist)) || (!deleted && e == nil && current == publication.Target))
	if deleted && published {
		if _, e = os.Stat(tomb); e != nil && record.Phase != "published" && record.Phase != "indexed" {
			return subtitleError("subtitle_changed")
		}
	}
	if !published {
		if record.ProbeRevision != media.SubtitleProbeRevision(f.Probe) {
			return subtitleError("subtitle_changed")
		}
		if e != nil {
			return e
		}
		if current != record.Fingerprint {
			return subtitleError("subtitle_changed")
		}
		safe, e := media.Within(w.Config.MediaRoot, source)
		if e != nil || safe != source {
			return subtitleError("subtitle_changed")
		}
		if track == nil {
			return subtitleError("subtitle_changed")
		}
		expected := video
		if track.ExternalPath != "" {
			expected, e = media.Within(w.Config.MediaRoot, track.ExternalPath)
			if e != nil {
				return e
			}
		}
		if expected != source {
			return subtitleError("subtitle_changed")
		}
		if e = subtitleWritable(source); e != nil {
			return e
		}
		if source == video {
			if !strings.EqualFold(filepath.Ext(video), ".mkv") {
				return media.ErrSubtitleFormat
			}
			if e = w.remuxSubtitle(ctx, video, tmp, *track, record.Action, int(record.OffsetMs)); e != nil {
				return e
			}
		} else if !deleted {
			raw, e := readSubtitleFile(source)
			if e != nil {
				return e
			}
			updated, e := media.ShiftSubtitleDocument(string(raw), strings.TrimPrefix(strings.ToLower(filepath.Ext(source)), "."), int(record.OffsetMs))
			if e != nil {
				return e
			}
			if e = os.WriteFile(tmp, []byte(updated), 0600); e != nil {
				return e
			}
		}
		publication.Delete = deleted
		if !deleted {
			if e = audio.PreserveMode(source, tmp); e != nil {
				return e
			}
			if e = audio.SyncFile(tmp); e != nil {
				return e
			}
			publication.Target, e = audio.FingerprintAt(tmp, source)
			if e != nil {
				return e
			}
		}
		data, _ = json.Marshal(publication)
		if e = audio.WriteJournal(journal, data); e != nil {
			return e
		}
		record.Phase = "prepared"
		if e = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: j.ID, Phase: record.Phase}); e != nil {
			return e
		}
		if e = w.validLease(ctx, j); e != nil {
			return e
		}
		if e = w.subtitlePermission(ctx, record.UserID, item.LibraryID); e != nil {
			return e
		}
		check, e := audio.Fingerprint(source)
		if e != nil {
			return e
		}
		if check != current {
			return subtitleError("subtitle_changed")
		}
		if deleted {
			e = os.Rename(source, tomb)
		} else {
			e = os.Rename(tmp, source)
		}
		if e != nil {
			return e
		}
		if e = audio.SyncDirectory(filepath.Dir(source)); e != nil {
			return e
		}
	}
	record.Phase = "published"
	if e = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: j.ID, Phase: record.Phase}); e != nil {
		return e
	}
	if e = w.refreshSubtitleFile(ctx, f, video); e != nil {
		return e
	}
	if e = os.Remove(tomb); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = audio.SyncDirectory(filepath.Dir(source)); e != nil {
		return e
	}
	record.Phase = "indexed"
	if e = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: j.ID, Phase: record.Phase}); e != nil {
		return e
	}
	_ = os.Remove(tmp)
	_ = os.Remove(journal)
	return nil
}
func readSubtitleFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 524289))
	if len(b) > 524288 {
		return nil, media.ErrSubtitleFormat
	}
	return b, e
}
func (w *Worker) subtitleCues(ctx context.Context, source string, track media.Stream) ([]media.Cue, error) {
	if track.ExternalPath != "" {
		data, e := readSubtitleFile(source)
		if e != nil {
			return nil, e
		}
		if _, e = media.ShiftSubtitleDocument(string(data), track.Codec, 0); e != nil {
			return nil, e
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	dir := filepath.Join(w.Config.CacheRoot, "subtitles")
	if e := os.MkdirAll(dir, 0750); e != nil {
		return nil, e
	}
	tmp, e := os.CreateTemp(dir, "preview-*.vtt")
	if e != nil {
		return nil, e
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	selection := "0:" + strconv.Itoa(track.Index)
	if track.ExternalPath != "" {
		selection = "0:0"
	}
	if e = exec.CommandContext(runCtx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", source, "-map", selection, "-c:s", "webvtt", "-fs", "524289", tmp.Name()).Run(); e != nil {
		return nil, e
	}
	data, e := readSubtitleFile(tmp.Name())
	if e != nil {
		return nil, e
	}
	return media.ParseSubtitles(string(data))
}
func (w *Worker) discoverSubtitleSidecars(path string, probe *media.Probe) error {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	siblings, e := os.ReadDir(filepath.Dir(path))
	if e != nil {
		return e
	}
	for _, side := range siblings {
		ext := strings.ToLower(filepath.Ext(side.Name()))
		if side.IsDir() || (ext != ".srt" && ext != ".vtt" && ext != ".ass" && ext != ".ssa") {
			continue
		}
		sidePath := filepath.Join(filepath.Dir(path), side.Name())
		stem := strings.TrimSuffix(sidePath, ext)
		if stem != base && !strings.HasPrefix(stem, base+".") {
			continue
		}
		safe, e := media.Within(w.Config.MediaRoot, sidePath)
		if e != nil {
			continue
		}
		lang := strings.TrimPrefix(strings.TrimPrefix(stem, base), ".")
		probe.Streams = append(probe.Streams, media.Stream{Index: 10000 + len(probe.Streams), Type: "subtitle", Codec: strings.TrimPrefix(ext, "."), ExternalPath: safe, Tags: map[string]string{"language": lang}})
	}
	return nil
}
func (w *Worker) refreshSubtitleFile(ctx context.Context, file store.MediaFile, path string) error {
	probe, e := media.Inspect(ctx, path)
	if e != nil {
		return e
	}
	if e = w.discoverSubtitleSidecars(path, &probe); e != nil {
		return e
	}
	// Removing/reordering a stream must not leave old prepared JSON accessible.
	var old media.Probe
	_ = json.Unmarshal(file.Probe, &old)
	for _, s := range old.Streams {
		if s.SubtitleID != "" {
			_ = os.Remove(filepath.Join(w.Config.CacheRoot, "subtitles", s.SubtitleID+".json"))
		}
	}
	w.prepareTextSubtitles(ctx, file.ID, path, &probe, false)
	data, e := json.Marshal(probe)
	if e != nil {
		return e
	}
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	// An original timing edit establishes a new default for all viewers.
	// Reset source-track overrides atomically with its refreshed catalog entry.
	tx, e := w.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := w.DB.WithTx(tx)
	if e = q.UpdateSubtitleFileProbe(ctx, store.UpdateSubtitleFileProbeParams{ID: file.ID, Probe: data, Size: st.Size(), ModifiedAt: st.ModTime().UnixNano()}); e != nil {
		return e
	}
	if e = q.ClearOriginalSubtitleTiming(ctx, file.ID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Terminal jobs may have lost their lease between publication and indexing.
// Recovery only completes a proven publication, or abandons uncommitted output;
// it never applies another edit after permission/configuration changes.
func (w *Worker) recoverSubtitles(ctx context.Context) {
	records, e := w.DB.RecoverableSubtitleJobs(ctx)
	if e != nil {
		return
	}
	for _, r := range records {
		func() {
			f, e := w.DB.GetFile(ctx, r.FileID)
			if e != nil {
				return
			}
			video, e := media.Within(w.Config.MediaRoot, f.Path)
			if e != nil {
				return
			}
			unlock, e := audio.Lock(ctx, w.Config.CacheRoot, video)
			if e != nil {
				return
			}
			defer unlock()
			if filepath.Dir(r.SourcePath) != filepath.Dir(video) {
				return
			}
			root, e := filepath.EvalSymlinks(w.Config.MediaRoot)
			if e != nil {
				return
			}
			rel, e := filepath.Rel(root, r.SourcePath)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return
			}
			if r.SourcePath != video {
				if !media.SubtitleSidecarSource(video, r.SourcePath) {
					return
				}
				unlockSource, e := audio.Lock(ctx, w.Config.CacheRoot, r.SourcePath)
				if e != nil {
					return
				}
				defer unlockSource()
			}
			prefix := filepath.Join(filepath.Dir(r.SourcePath), ".jfe-subtitle-"+r.ID)
			journal, tmp, tomb := prefix+".json", prefix+filepath.Ext(r.SourcePath), prefix+".old"
			if r.Phase == "indexed" {
				_ = os.Remove(tmp)
				_ = os.Remove(tomb)
				_ = os.Remove(journal)
				_ = w.DB.CompleteRecoveredSubtitleJob(ctx, r.ID)
				return
			}
			data, e := os.ReadFile(journal)
			if errors.Is(e, os.ErrNotExist) && (r.Phase == "pending" || r.Phase == "ready") {
				_ = os.Remove(tmp)
				_ = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: r.ID, Phase: "abandoned", ErrorCode: r.ErrorCode})
				return
			}
			if e != nil {
				return
			}
			var publication struct {
				Target string `json:"target"`
				Delete bool   `json:"delete"`
			}
			if json.Unmarshal(data, &publication) != nil {
				return
			}
			current, e := audio.Fingerprint(r.SourcePath)
			committed := (!publication.Delete && e == nil && current == publication.Target) || (publication.Delete && errors.Is(e, os.ErrNotExist))
			if !committed {
				if e != nil || current != r.Fingerprint {
					_ = os.Remove(tmp)
					// Never overwrite a source recreated/edited outside JFE. Retain a
					// deletion tombstone for manual recovery when publication is ambiguous.
					_ = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: r.ID, Phase: "abandoned", ErrorCode: "subtitle_changed"})
					return
				}
				_ = os.Remove(tmp)
				_ = os.Remove(journal)
				_ = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: r.ID, Phase: "abandoned", ErrorCode: "subtitle_job_failed"})
				return
			}
			if e = w.refreshSubtitleFile(ctx, f, video); e != nil {
				return
			}
			if e = os.Remove(tomb); e != nil && !errors.Is(e, os.ErrNotExist) {
				return
			}
			if e = audio.SyncDirectory(filepath.Dir(r.SourcePath)); e != nil {
				return
			}
			if e = w.DB.SetSubtitleSyncPhase(ctx, store.SetSubtitleSyncPhaseParams{ID: r.ID, Phase: "indexed"}); e != nil {
				return
			}
			_ = os.Remove(tmp)
			_ = os.Remove(journal)
			_ = w.DB.CompleteRecoveredSubtitleJob(ctx, r.ID)
		}()
	}
}

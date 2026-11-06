package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"jfe/backend/internal/subtitleprovider"
)

type autoSubtitlePublication struct {
	Video             string `json:"video"`
	Fingerprint       string `json:"fingerprint"`
	Temp              string `json:"temp"`
	Target            string `json:"target"`
	Digest            string `json:"digest"`
	OutputFingerprint string `json:"outputFingerprint"`
}

func (w *Worker) scheduleAutoSubtitles(ctx context.Context) error {
	// Recovery is also scheduled when downloads have subsequently been disabled.
	recoverable, e := w.DB.RecoverableAutoSubtitles(ctx)
	if e != nil {
		return e
	}
	for _, id := range recoverable {
		if _, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "subtitle_auto", ResourceID: id, Payload: []byte("{}")}); e != nil {
			return e
		}
	}
	if !w.Config.SubtitleDownloads || w.Config.OpenSubtitlesKey == "" {
		return nil
	}
	if e = w.DB.InitializeAutoSubtitles(ctx); e != nil {
		return e
	}
	files, e := w.DB.DueAutoSubtitles(ctx)
	if e != nil {
		return e
	}
	for _, id := range files {
		if _, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "subtitle_auto", ResourceID: id, Payload: []byte("{}")}); e != nil {
			return e
		}
	}
	return nil
}

func (w *Worker) autoSubtitleState(ctx context.Context, id, state, code string, at time.Time) error {
	jobStage(ctx, "subtitle_"+state)
	return w.DB.SetAutoSubtitleState(ctx, store.SetAutoSubtitleStateParams{FileID: id, State: state, ErrorCode: code, NextSearchAt: at})
}

func (w *Worker) autoSubtitleProbe(ctx context.Context, f store.MediaFile, path string) (media.Probe, error) {
	var p media.Probe
	if e := json.Unmarshal(f.Probe, &p); e != nil {
		return p, e
	}
	e := w.discoverSubtitleSidecars(path, &p)
	return p, e
}
func hasVietnamese(p media.Probe) bool {
	for _, track := range p.Streams {
		if track.Type == "subtitle" && (subtitleprovider.Vietnamese(track.Tags["language"]) || subtitleprovider.Vietnamese(track.Tags["title"])) {
			return true
		}
	}
	return false
}

func (w *Worker) indexAutoSubtitle(ctx context.Context, f store.MediaFile, path string) error {
	p, e := w.autoSubtitleProbe(ctx, f, path)
	if e != nil {
		return e
	}
	w.prepareTextSubtitles(ctx, f.ID, path, &p, true)
	if e = ctx.Err(); e != nil {
		return e
	}
	data, e := json.Marshal(p)
	if e != nil {
		return e
	}
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	// Appending a new source must preserve existing personal timing offsets.
	return w.DB.UpdateSubtitleFileProbe(ctx, store.UpdateSubtitleFileProbeParams{ID: f.ID, Probe: data, Size: st.Size(), ModifiedAt: st.ModTime().UnixNano()})
}

func syncSubtitleDirectory(path string) error {
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}

func (w *Worker) recoverAutoSubtitle(ctx context.Context, f store.MediaFile, path string, raw []byte) error {
	jobStage(ctx, "recover_subtitle_publication")
	var p autoSubtitlePublication
	if e := json.Unmarshal(raw, &p); e != nil {
		return e
	}
	target := strings.TrimSuffix(path, filepath.Ext(path)) + ".vi.srt"
	if p.Video != path || p.Target != target || filepath.Dir(p.Temp) != filepath.Dir(path) || !strings.HasPrefix(filepath.Base(p.Temp), ".jfe-auto-subtitle-") {
		return errors.New("invalid subtitle publication journal")
	}
	// Only our recorded regular file can prove publication. Never alter a foreign
	// file or an outside edit, even when its text happens to resemble our output.
	published := false
	if st, e := os.Lstat(target); e == nil && st.Mode().IsRegular() {
		data, e := readSubtitleFile(target)
		if e == nil {
			fingerprint, fpErr := audio.Fingerprint(target)
			published = fpErr == nil && fingerprint == p.OutputFingerprint && fmt.Sprintf("%x", sha256.Sum256(data)) == p.Digest
		}
	}
	fp, e := audio.Fingerprint(path)
	if e != nil {
		return e
	}
	if published && fp == p.Fingerprint {
		if e = w.indexAutoSubtitle(ctx, f, path); e != nil {
			return e
		}
	}
	if e = os.Remove(p.Temp); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = syncSubtitleDirectory(target); e != nil {
		return e
	}
	state, code := "pending", ""
	if published && fp == p.Fingerprint {
		state = "available"
	} else {
		code = "publication_changed"
		state = "error"
	}
	if e = w.autoSubtitleState(ctx, f.ID, state, code, time.Now().Add(24*time.Hour)); e != nil {
		return e
	}
	return w.DB.ClearAutoSubtitlePublication(ctx, f.ID)
}

func (w *Worker) automaticSubtitle(ctx context.Context, j store.Job) (err error) {
	f, e := w.DB.GetFile(ctx, j.ResourceID)
	if e != nil {
		return e
	}
	path, e := media.Within(w.Config.MediaRoot, f.Path)
	if e != nil {
		return e
	}
	unlock, e := audio.Lock(ctx, w.Config.CacheRoot, path)
	if e != nil {
		return e
	}
	defer unlock()
	f, e = w.DB.GetFile(ctx, f.ID)
	if e != nil {
		return e
	}
	record, e := w.DB.GetAutoSubtitle(ctx, f.ID)
	if e != nil {
		return e
	}
	if string(record.Publication) != "{}" {
		e = w.recoverAutoSubtitle(ctx, f, path, record.Publication)
		if e != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = w.autoSubtitleState(cleanup, f.ID, "recovery_error", "publication_changed", time.Now().Add(5*time.Minute))
		}
		return e
	}
	next := time.Now().Add(24 * time.Hour)
	defer func() {
		if err == nil {
			return
		}
		code := "subtitle_download_failed"
		state := "error"
		at := next
		var pe *subtitleprovider.Error
		if errors.As(err, &pe) {
			code = pe.Code
			if pe.RetryAt.After(time.Now()) {
				at = pe.RetryAt
			}
			state = "waiting"
		}
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS) {
			code = "read_only"
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = w.autoSubtitleState(cleanup, f.ID, state, code, at)
	}()
	item, e := w.DB.GetItem(ctx, f.ItemID)
	if e != nil {
		return e
	}
	library, e := w.DB.GetLibrary(ctx, item.LibraryID)
	if e != nil {
		return e
	}
	if !w.Config.SubtitleDownloads || !library.AutoSubtitles || !f.Available {
		return w.autoSubtitleState(ctx, f.ID, "disabled", "", next)
	}
	if item.Kind != "movie" && item.Kind != "episode" {
		return errors.New("unsupported subtitle media")
	}
	// Library path changes must not authorize publication beside an old file.
	inside := false
	for _, root := range library.Paths {
		if _, e := media.Within(root, path); e == nil {
			inside = true
			break
		}
	}
	if !inside {
		return errors.New("subtitle video outside library")
	}
	fp, e := audio.Fingerprint(path)
	if e != nil {
		return e
	}
	probe, e := w.autoSubtitleProbe(ctx, f, path)
	if e != nil {
		return e
	}
	if hasVietnamese(probe) {
		if e = w.indexAutoSubtitle(ctx, f, path); e != nil {
			return e
		}
		return w.autoSubtitleState(ctx, f.ID, "available", "", next)
	}
	target := strings.TrimSuffix(path, filepath.Ext(path)) + ".vi.srt"
	if _, e = os.Lstat(target); e == nil {
		return w.autoSubtitleState(ctx, f.ID, "error", "destination_exists", next)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	// Verify directory writability before spending provider download quota.
	tempPath := filepath.Join(filepath.Dir(path), ".jfe-auto-subtitle-"+f.ID)
	// A killed worker may leave an unpublished staging file. The per-video lock
	// and absence of a journal prove it is safe to discard on the next attempt.
	if e = os.Remove(tempPath); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	temp, e := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	searchTarget := subtitleprovider.SearchTarget{Title: item.Title, Year: int(item.Year), Kind: item.Kind, Season: int(item.Season), Episode: int(item.Episode)}
	searchTarget.TMDBID, _ = strconv.Atoi(item.ProviderID)
	if item.Kind == "episode" {
		series, err := w.DB.GetItem(ctx, item.ParentID)
		if err != nil {
			return err
		}
		searchTarget.Title, searchTarget.Year = series.Title, int(series.Year)
		searchTarget.TMDBID, _ = strconv.Atoi(series.ProviderID)
	}
	searchTarget = searchTarget.WithFilenameFallback(path)
	searchTarget = subtitleprovider.ResolveSearchTarget(ctx, w.Config.TMDBToken, searchTarget)
	if strings.TrimSpace(searchTarget.Title) == "" {
		return w.autoSubtitleState(ctx, f.ID, "unmatched", "", next)
	}
	query := searchTarget.Query("vi", "")
	log.Ctx(ctx).Info().Str("subtitleSearchTitle", searchTarget.Title).Int("subtitleSearchYear", searchTarget.Year).Msg("Searching automatic subtitles using resolved title")
	if e = w.autoSubtitleState(ctx, f.ID, "searching", "", next); e != nil {
		return e
	}
	const maxCandidateDownloads = 3
	var selected subtitleprovider.Candidate
	var cues []media.Cue
	found := false
	e = subtitleprovider.Do(ctx, w.Pool, w.Config, func(ctx context.Context, client *subtitleprovider.Client) error {
		candidates, err := client.Search(ctx, query)
		if err != nil {
			return err
		}
		ranked := subtitleprovider.RankCandidates(candidates, searchTarget)
		log.Ctx(ctx).Info().Int("searchResults", len(candidates)).Int("matchedCandidates", len(ranked)).Int("candidateLimit", maxCandidateDownloads).Msg("Automatic subtitle candidates ranked")
		if len(ranked) == 0 {
			return nil
		}
		if err = w.autoSubtitleState(ctx, f.ID, "downloading", "", next); err != nil {
			return err
		}
		var lastErr error
		for i, candidate := range ranked[:min(len(ranked), maxCandidateDownloads)] {
			if err = ctx.Err(); err != nil {
				return err
			}
			log.Ctx(ctx).Info().Int("providerFileId", candidate.FileID).Int("candidateAttempt", i+1).Int("candidateLimit", maxCandidateDownloads).Msg("Trying automatic subtitle candidate")
			_, downloaded, err := client.Download(ctx, candidate.FileID)
			if err == nil {
				selected, cues, found = candidate, downloaded, true
				log.Ctx(ctx).Info().Int("providerFileId", candidate.FileID).Int("candidateAttempt", i+1).Msg("Automatic subtitle candidate accepted")
				return nil
			}
			lastErr = err
			var pe *subtitleprovider.Error
			if !errors.As(err, &pe) || pe.Reason != "invalid_subtitle_response" {
				// Network, authentication, rate and quota failures are shared;
				// do not spend more quota attempting other files in that case.
				return err
			}
			log.Ctx(ctx).Warn().Int("providerFileId", candidate.FileID).Int("candidateAttempt", i+1).Object("provider", pe).Msg("Automatic subtitle candidate invalid")
			if client.Remaining == 0 {
				return err
			}
		}
		return lastErr
	})
	if e != nil {
		return e
	}
	if !found {
		return w.autoSubtitleState(ctx, f.ID, "unmatched", "", next)
	}
	data, e := subtitleprovider.RenderSRT(cues)
	if e != nil {
		return e
	}
	if e = temp.Chmod(0644); e != nil {
		return e
	}
	if _, e = temp.Write(data); e != nil {
		return e
	}
	if e = temp.Sync(); e != nil {
		return e
	}
	if e = temp.Close(); e != nil {
		return e
	}
	if e = syncSubtitleDirectory(tempPath); e != nil {
		return e
	}
	outputFingerprint, e := audio.FingerprintAt(tempPath, target)
	if e != nil {
		return e
	}
	publication := autoSubtitlePublication{Video: path, Fingerprint: fp, Temp: tempPath, Target: target, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), OutputFingerprint: outputFingerprint}
	raw, e := json.Marshal(publication)
	if e != nil {
		return e
	}
	// A durable journal blocks library deletion before a sidecar can be published.
	jobStage(ctx, "prepare_subtitle_publication")
	tx, e := w.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := w.DB.WithTx(tx)
	if e = q.LockSubtitleLibrary(ctx, item.LibraryID); e != nil {
		return e
	}
	if e = q.LockSubtitleFile(ctx, f.ID); e != nil {
		return e
	}
	current, e := q.LockAutoSubtitleJob(ctx, j.ID)
	if e != nil {
		return e
	}
	if current.LeaseID != j.LeaseID || current.State != "running" || current.CancelRequested || !current.LeaseUntil.After(time.Now()) {
		return context.Canceled
	}
	library, e = q.LockAutoSubtitleLibrary(ctx, item.LibraryID)
	if e != nil {
		return e
	}
	if !library.AutoSubtitles {
		return w.autoSubtitleState(ctx, f.ID, "disabled", "", next)
	}
	busy, e := q.SubtitleFileBusy(ctx, f.ID)
	if e != nil {
		return e
	}
	if busy {
		return errors.New("subtitle edit in progress")
	}
	actual, e := audio.Fingerprint(path)
	if e != nil {
		return e
	}
	if actual != fp {
		return errors.New("subtitle video changed")
	}
	fresh, e := w.autoSubtitleProbe(ctx, f, path)
	if e != nil {
		return e
	}
	if hasVietnamese(fresh) {
		return w.autoSubtitleState(ctx, f.ID, "available", "", next)
	}
	if e = q.SetAutoSubtitlePublication(ctx, store.SetAutoSubtitlePublicationParams{FileID: f.ID, Publication: raw, ProviderFileID: int32(selected.FileID)}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	keep = true
	if e = w.publishAutoSubtitle(ctx, j, f, item.LibraryID, publication); e != nil {
		return e
	}
	return w.recoverAutoSubtitle(ctx, f, path, raw)
}

// Reacquire publication guards after committing the journal. Policy changes,
// cancellation and lease reclamation serialize with this short critical section.
func (w *Worker) publishAutoSubtitle(ctx context.Context, j store.Job, f store.MediaFile, libraryID string, p autoSubtitlePublication) error {
	jobStage(ctx, "publish_subtitle")
	tx, e := w.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := w.DB.WithTx(tx)
	if e = q.LockSubtitleLibrary(ctx, libraryID); e != nil {
		return e
	}
	if e = q.LockSubtitleFile(ctx, f.ID); e != nil {
		return e
	}
	library, e := q.LockAutoSubtitleLibrary(ctx, libraryID)
	if e != nil {
		return e
	}
	current, e := q.LockAutoSubtitleJob(ctx, j.ID)
	if e != nil {
		return e
	}
	if !library.AutoSubtitles || !w.Config.SubtitleDownloads || current.LeaseID != j.LeaseID || current.CancelRequested || current.State != "running" || !current.LeaseUntil.After(time.Now()) {
		return context.Canceled
	}
	inside := false
	for _, root := range library.Paths {
		if safe, e := media.Within(root, p.Video); e == nil && safe == p.Video {
			inside = true
			break
		}
	}
	if !inside {
		return errors.New("subtitle video outside library")
	}
	fresh, e := q.GetFile(ctx, f.ID)
	if e != nil {
		return e
	}
	if !fresh.Available || fresh.Path != f.Path {
		return errors.New("subtitle video changed")
	}
	fp, e := audio.Fingerprint(p.Video)
	if e != nil {
		return e
	}
	if fp != p.Fingerprint {
		return errors.New("subtitle video changed")
	}
	busy, e := q.SubtitleFileBusy(ctx, f.ID)
	if e != nil {
		return e
	}
	if busy {
		return errors.New("subtitle edit in progress")
	}
	probe, e := w.autoSubtitleProbe(ctx, fresh, p.Video)
	if e != nil {
		return e
	}
	if hasVietnamese(probe) {
		return errors.New("subtitle already available")
	}
	st, e := os.Lstat(p.Temp)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("subtitle temporary file changed")
	}
	fp, e = audio.FingerprintAt(p.Temp, p.Target)
	if e != nil {
		return e
	}
	if fp != p.OutputFingerprint {
		return errors.New("subtitle temporary file changed")
	}
	// link(2) atomically publishes the complete file and never replaces a target.
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = os.Link(p.Temp, p.Target); e != nil {
		return e
	}
	if e = syncSubtitleDirectory(p.Target); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

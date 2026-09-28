package worker

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (w *Worker) cleanupAudio(ctx context.Context) {
	if w.Role == "downloader" && w.Config.ImportRoot != "" {
		dir, e := media.Within(w.Config.ImportRoot, filepath.Join(w.Config.ImportRoot, ".staging"))
		if e != nil {
			return
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if len(name) < 74 || !entry.IsDir() {
				continue
			}
			st, e := entry.Info()
			if e != nil || time.Since(st.ModTime()) < 2*time.Minute {
				continue
			}
			id, lease := name[:36], name[37:73]
			j, e := w.DB.GetJob(ctx, id)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e == nil && j.State == "running" && j.LeaseID == lease && j.LeaseUntil.After(time.Now()) {
				continue
			}
			safe, e := media.Within(dir, filepath.Join(dir, name))
			if e == nil {
				_ = os.RemoveAll(safe)
			}
		}
		return
	}
	if w.Role != "scanner" {
		return
	}
	jobs, e := w.DB.AbandonedTagJobs(ctx)
	if e != nil {
		return
	}
	for _, j := range jobs {
		f, e := w.DB.GetFile(ctx, j.FileID)
		if e != nil {
			continue
		}
		path, e := audio.Within(w.Config.MediaRoot, w.Config.ImportRoot, f.Path)
		if e != nil {
			continue
		}
		func() {
			lockCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			unlock, e := audio.Lock(lockCtx, w.Config.CacheRoot, path)
			if e != nil {
				return
			}
			defer unlock()
			base := filepath.Join(filepath.Dir(path), ".jfe-tag-"+j.ID)
			if data, e := os.ReadFile(base + ".json"); e == nil {
				fingerprint, e := audio.Fingerprint(path)
				if e == nil && strings.TrimSpace(string(data)) == fingerprint {
					item, e := w.DB.GetItem(ctx, f.ItemID)
					if e != nil {
						return
					}
					library, e := w.DB.GetLibrary(ctx, item.LibraryID)
					if e != nil {
						return
					}
					if e = w.indexAudio(ctx, library, path); e != nil {
						return
					}
				}
			}
			_ = os.Remove(base + filepath.Ext(path))
			_ = os.Remove(base + ".json")
			_ = os.Remove(base + ".json.tmp")
			_ = w.DB.SetTagPhase(ctx, store.SetTagPhaseParams{ID: j.ID, Phase: "abandoned"})
		}()
	}
}

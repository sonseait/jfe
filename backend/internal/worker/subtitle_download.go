package worker

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/store"
	"jfe/backend/internal/subtitleprovider"
)

func (w *Worker) downloadSubtitle(ctx context.Context, j store.Job) error {
	var p struct {
		UserID         string `json:"userId"`
		FileID         string `json:"fileId"`
		ProviderFileID int    `json:"providerFileId"`
	}
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return err
	}
	if p.ProviderFileID < 1 {
		return errors.New("invalid subtitle provider file")
	}
	unlock, err := audio.Lock(ctx, w.Config.CacheRoot, "subtitle-download:"+j.ResourceID)
	if err != nil {
		return err
	}
	defer unlock()
	checkAccess := func() error {
		f, err := w.DB.GetFile(ctx, p.FileID)
		if err != nil {
			return err
		}
		if !f.Available {
			return errors.New("media file unavailable")
		}
		i, err := w.DB.GetItem(ctx, f.ItemID)
		if err != nil {
			return err
		}
		u, err := w.DB.GetUser(ctx, p.UserID)
		if err != nil {
			return err
		}
		if u.Disabled || (i.Kind != "movie" && i.Kind != "episode") {
			return errors.New("subtitle access denied")
		}
		allowed, err := w.DB.CanAccess(ctx, store.CanAccessParams{ID: i.LibraryID, UserID: u.ID, Column3: u.Role == "admin"})
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("subtitle access denied")
		}
		return nil
	}
	if err := checkAccess(); err != nil {
		return err
	}
	_, err = w.DB.GetSubtitle(ctx, store.GetSubtitleParams{ID: j.ResourceID, FileID: p.FileID, UserID: p.UserID})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	name, cues, err := (subtitleprovider.Client{Key: w.Config.OpenSubtitlesKey, Token: w.Config.OpenSubtitlesToken}).Download(ctx, p.ProviderFileID)
	if err != nil {
		return err
	}
	if err = checkAccess(); err != nil {
		return err
	}
	data, err := json.Marshal(cues)
	if err != nil {
		return err
	}
	_, err = w.DB.SaveDownloadedSubtitle(ctx, store.SaveDownloadedSubtitleParams{ID: j.ResourceID, FileID: p.FileID, UserID: p.UserID, Name: name, Cues: data})
	return err
}

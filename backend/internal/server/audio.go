package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CoverArtDTO struct {
	Artwork string `json:"artwork"`
}
type AudioArtistsDTO struct {
	Items []string `json:"items"`
}
type AudioTagsDTO audio.Tags
type AudioTagPatch audio.Patch
type AudioChapterDTO struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type AudioMetadataDTO struct {
	Tags     AudioTagsDTO      `json:"tags"`
	Chapters []AudioChapterDTO `json:"chapters"`
}
type FileTagsDTO struct {
	FileID      string       `json:"fileId"`
	Fingerprint string       `json:"fingerprint"`
	Writable    bool         `json:"writable"`
	Tags        AudioTagsDTO `json:"tags"`
}
type TagChange struct {
	FileID      string        `json:"fileId" jsonschema:"format=uuid"`
	Fingerprint string        `json:"fingerprint" jsonschema:"minLength=64,maxLength=64"`
	Patch       AudioTagPatch `json:"patch,omitempty"`
}
type TagChanges struct {
	Patch *AudioTagPatch `json:"patch,omitempty"`
	Items []TagChange    `json:"items" jsonschema:"minItems=1,maxItems=100"`
}
type ImportRequest struct {
	LibraryID string `json:"libraryId" jsonschema:"format=uuid"`
	URL       string `json:"url" jsonschema:"minLength=1,maxLength=2048"`
	Follow    bool   `json:"follow"`
}
type SourceUpdate struct {
	Follow bool `json:"follow"`
	Paused bool `json:"paused"`
}
type ImportEntryDTO struct {
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Ordinal int    `json:"ordinal"`
	State   string `json:"state"`
	Error   string `json:"error"`
}
type ImportSourceDTO struct {
	ID         string           `json:"id"`
	LibraryID  string           `json:"libraryId"`
	UserID     string           `json:"userId"`
	URL        string           `json:"url"`
	Title      string           `json:"title"`
	Follow     bool             `json:"follow"`
	Paused     bool             `json:"paused"`
	NextSyncAt time.Time        `json:"nextSyncAt"`
	Entries    []ImportEntryDTO `json:"entries"`
}
type ImportSourcesDTO struct {
	Items []ImportSourceDTO `json:"items"`
}
type DownloadSettingsDTO struct {
	MaxConcurrent int `json:"maxConcurrent" jsonschema:"minimum=1,maximum=8"`
	MinFreeGiB    int `json:"minFreeGiB" jsonschema:"minimum=1,maximum=100000"`
}
type MusicSearchQuery struct {
	Search string `json:"search" jsonschema:"minLength=1,maxLength=200"`
}
type MusicMatchDTO struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Date    string `json:"date"`
	Country string `json:"country"`
}
type MusicMatchesDTO struct {
	Items []MusicMatchDTO `json:"items"`
}

func (s *Server) canImport(ctx context.Context, id string) error {
	ok, e := s.DB.CanImport(ctx, store.CanImportParams{LibraryID: id, UserID: route.User(ctx).ID})
	if e != nil {
		return e
	}
	if !ok {
		return route.Fail(403, "Import and tag editing permission required")
	}
	return nil
}
func (s *Server) sourceDTO(ctx context.Context, v store.ImportSource) (ImportSourceDTO, error) {
	d := ImportSourceDTO{ID: v.ID, LibraryID: v.LibraryID, UserID: v.UserID, URL: v.Url, Title: v.Title, Follow: v.Follow, Paused: v.Paused, NextSyncAt: v.NextSyncAt, Entries: []ImportEntryDTO{}}
	entries, e := s.DB.ListImportEntries(ctx, v.ID)
	for _, entry := range entries {
		d.Entries = append(d.Entries, ImportEntryDTO{entry.VideoID, entry.Title, int(entry.Ordinal), entry.State, entry.Error})
	}
	return d, e
}
func (s *Server) ownSource(ctx context.Context, id string) (store.ImportSource, error) {
	v, e := s.DB.GetImportSource(ctx, id)
	if e != nil {
		return v, e
	}
	if route.User(ctx).Role != "admin" && v.UserID != route.User(ctx).ID {
		return v, route.Fail(403, "Source access denied")
	}
	return v, s.canImport(ctx, v.LibraryID)
}
func (s *Server) audioRoutes() {
	r := s.Routes
	route.Get(r, op("/audio/musicbrainz/:id/cover", "getMusicBrainzCover", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[CoverArtDTO], error) {
		b, e := audio.CoverArt(ctx, in.Params.ID)
		if e != nil {
			return out(CoverArtDTO{}, route.Fail(404, "Cover artwork unavailable"))
		}
		return out(CoverArtDTO{Artwork: base64.StdEncoding.EncodeToString(b)}, nil)
	})

	route.Get(r, op("/libraries/:id/artists", "listAudioArtists", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[AudioArtistsDTO], error) {
		if e := s.access(ctx, in.Params.ID); e != nil {
			return out(AudioArtistsDTO{}, e)
		}
		rows, e := s.DB.ListAudioArtists(ctx, in.Params.ID)
		return out(AudioArtistsDTO{Items: rows}, e)
	})

	route.Get(r, op("/files/:id/tags", "getAudioTags", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[FileTagsDTO], error) {
		f, e := s.DB.GetFile(ctx, in.Params.ID)
		if e != nil {
			return out(FileTagsDTO{}, e)
		}
		i, e := s.authorizedItem(ctx, f.ItemID)
		if e != nil {
			return out(FileTagsDTO{}, e)
		}
		if !audio.IsItem(i.Kind) {
			return out(FileTagsDTO{}, route.Fail(422, "Not an audio file"))
		}
		path, e := audio.Within(s.Config.MediaRoot, s.Config.ImportRoot, f.Path)
		if e != nil {
			return out(FileTagsDTO{}, route.Fail(404, "File unavailable"))
		}
		fingerprint, e := audio.Fingerprint(path)
		if e != nil {
			return out(FileTagsDTO{}, e)
		}
		v, e := s.DB.GetAudioMetadata(ctx, i.ID)
		d := FileTagsDTO{FileID: f.ID, Fingerprint: fingerprint, Writable: audio.Supported(path) && s.canImport(ctx, i.LibraryID) == nil}
		if e == nil {
			e = json.Unmarshal(v.Tags, &d.Tags)
		}
		return out(d, e)
	})
	route.Post(r, op("/audio/tags", "writeAudioTags", "user"), func(ctx context.Context, in route.Input[TagChanges, route.Empty, route.Empty]) (route.Output[JobsDTO], error) {
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return out(JobsDTO{}, e)
		}
		defer tx.Rollback(ctx)
		q := s.DB.WithTx(tx)
		v := JobsDTO{Items: []JobDTO{}}
		seen := map[string]bool{}
		for _, change := range in.Body.Items {
			if in.Body.Patch != nil {
				own, _ := json.Marshal(change.Patch)
				if string(own) != "{}" {
					return out(v, route.Fail(422, "Use shared or per-file tags, not both"))
				}
				change.Patch = *in.Body.Patch
			}
			if seen[change.FileID] {
				return out(v, route.Fail(422, "Duplicate file"))
			}
			seen[change.FileID] = true
			patchData, _ := json.Marshal(change.Patch)
			if string(patchData) == "{}" {
				return out(v, route.Fail(422, "Select at least one tag"))
			}
			f, e := q.GetFile(ctx, change.FileID)
			if e != nil {
				return out(v, e)
			}
			i, e := s.authorizedItem(ctx, f.ItemID)
			if e != nil {
				return out(v, e)
			}
			if e = s.canImport(ctx, i.LibraryID); e != nil {
				return out(v, e)
			}
			if !audio.IsItem(i.Kind) || !audio.Supported(f.Path) {
				return out(v, route.Fail(422, "Unsupported tag container"))
			}
			path, e := audio.Within(s.Config.MediaRoot, s.Config.ImportRoot, f.Path)
			if e != nil {
				return out(v, route.Fail(404, "File unavailable"))
			}
			fingerprint, e := audio.Fingerprint(path)
			if e != nil {
				return out(v, e)
			}
			if fingerprint != change.Fingerprint {
				return out(v, route.Fail(409, "File changed; reload tags"))
			}
			j, e := q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "audio_tags", ResourceID: f.ID, Payload: []byte("{}")})
			if e != nil {
				return out(v, e)
			}
			if _, e = q.GetTagJob(ctx, j.ID); e == nil {
				return out(v, route.Fail(409, "A tag edit is already pending"))
			}
			patch, _ := json.Marshal(change.Patch)
			if e = q.SaveTagJob(ctx, store.SaveTagJobParams{ID: j.ID, UserID: route.User(ctx).ID, FileID: f.ID, Fingerprint: fingerprint, Patch: patch}); e != nil {
				return out(v, e)
			}
			v.Items = append(v.Items, jobDTO(j))
		}
		return out(v, tx.Commit(ctx))
	})
	route.Get(r, op("/audio/jobs", "listAudioJobs", "user"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[JobsDTO], error) {
		rows, e := s.DB.ListAudioJobs(ctx, route.User(ctx).ID)
		v := JobsDTO{Items: []JobDTO{}}
		for _, j := range rows {
			d := jobDTO(j)
			if j.Kind == "audio_tags" {
				if file, err := s.DB.GetFile(ctx, j.ResourceID); err == nil {
					d.ResourceName = filepath.Base(file.Path)
					d.ItemID = file.ItemID
				}
			} else {
				if source, err := s.DB.GetImportSource(ctx, j.ResourceID); err == nil {
					d.ResourceName = source.Title
					d.LibraryID = source.LibraryID
				}
			}
			v.Items = append(v.Items, d)
		}
		return out(v, e)
	})
	route.Post(r, op("/audio/jobs/:id/cancel", "cancelAudioJob", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		rows, e := s.DB.ListAudioJobs(ctx, route.User(ctx).ID)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		for _, j := range rows {
			if j.ID == in.Params.ID {
				return out(HealthDTO{"ok"}, s.DB.CancelJob(ctx, j.ID))
			}
		}
		return out(HealthDTO{}, route.Fail(404, "Job not found"))
	})
	route.Post(r, op("/imports", "previewImport", "user"), func(ctx context.Context, in route.Input[ImportRequest, route.Empty, route.Empty]) (route.Output[ImportSourceDTO], error) {
		if e := s.canImport(ctx, in.Body.LibraryID); e != nil {
			return out(ImportSourceDTO{}, e)
		}
		if s.Config.ImportRoot == "" {
			return out(ImportSourceDTO{}, route.Fail(503, "Import root is not configured"))
		}
		u, e := audio.YouTubeURL(in.Body.URL)
		if e != nil {
			return out(ImportSourceDTO{}, route.Fail(422, "Invalid YouTube URL"))
		}
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return out(ImportSourceDTO{}, e)
		}
		defer tx.Rollback(ctx)
		q := s.DB.WithTx(tx)
		v, e := q.CreateImportSource(ctx, store.CreateImportSourceParams{ID: uuid.NewString(), UserID: route.User(ctx).ID, LibraryID: in.Body.LibraryID, Url: u, Follow: in.Body.Follow})
		if e != nil {
			return out(ImportSourceDTO{}, e)
		}
		_, e = q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: "youtube_preview", ResourceID: v.ID, Payload: []byte("{}")})
		if e != nil {
			return out(ImportSourceDTO{}, e)
		}
		if e = tx.Commit(ctx); e != nil {
			return out(ImportSourceDTO{}, e)
		}
		d, e := s.sourceDTO(ctx, v)
		return out(d, e)
	})
	route.Get(r, op("/import-sources", "listImportSources", "user"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[ImportSourcesDTO], error) {
		rows, e := s.DB.ListImportSources(ctx, route.User(ctx).ID)
		v := ImportSourcesDTO{Items: []ImportSourceDTO{}}
		if e != nil {
			return out(v, e)
		}
		for _, row := range rows {
			d, e := s.sourceDTO(ctx, row)
			if e != nil {
				return out(v, e)
			}
			v.Items = append(v.Items, d)
		}
		return out(v, nil)
	})
	route.Put(r, op("/import-sources/:id", "updateImportSource", "user"), func(ctx context.Context, in route.Input[SourceUpdate, route.Empty, IDParams]) (route.Output[ImportSourceDTO], error) {
		_, e := s.ownSource(ctx, in.Params.ID)
		if e != nil {
			return out(ImportSourceDTO{}, e)
		}
		v, e := s.DB.UpdateImportSource(ctx, store.UpdateImportSourceParams{ID: in.Params.ID, Paused: in.Body.Paused, Follow: in.Body.Follow})
		if e != nil {
			return out(ImportSourceDTO{}, e)
		}
		if in.Body.Paused {
			if e = s.DB.CancelSourceJobs(ctx, v.ID); e != nil {
				return out(ImportSourceDTO{}, e)
			}
		}
		d, e := s.sourceDTO(ctx, v)
		return out(d, e)
	})
	route.Post(r, op("/import-sources/:id/download", "downloadImport", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[JobDTO], error) {
		v, e := s.ownSource(ctx, in.Params.ID)
		if e != nil {
			return out(JobDTO{}, e)
		}
		if v.Paused {
			return out(JobDTO{}, route.Fail(409, "Source is paused"))
		}
		d, e := s.enqueue(ctx, "downloader", "youtube_download", v.ID, nil)
		return out(d, e)
	})
	route.Post(r, op("/import-sources/:id/sync", "syncImport", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[JobDTO], error) {
		v, e := s.ownSource(ctx, in.Params.ID)
		if e != nil {
			return out(JobDTO{}, e)
		}
		if v.Paused {
			return out(JobDTO{}, route.Fail(409, "Source is paused"))
		}
		d, e := s.enqueue(ctx, "downloader", "youtube_preview", v.ID, nil)
		return out(d, e)
	})
	route.Delete(r, op("/import-sources/:id", "deleteImportSource", "user"), func(ctx context.Context, in route.Input[route.Empty, route.Empty, IDParams]) (route.Output[HealthDTO], error) {
		_, e := s.ownSource(ctx, in.Params.ID)
		if e != nil {
			return out(HealthDTO{}, e)
		}
		if e = s.DB.CancelSourceJobs(ctx, in.Params.ID); e != nil {
			return out(HealthDTO{}, e)
		}
		return out(HealthDTO{"ok"}, s.DB.DeleteImportSource(ctx, in.Params.ID))
	})
	route.Get(r, op("/admin/downloads", "getDownloadSettings", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[DownloadSettingsDTO], error) {
		b, e := s.DB.GetSetting(ctx, "downloads")
		v := DownloadSettingsDTO{}
		if e == nil {
			e = json.Unmarshal(b, &v)
		}
		return out(v, e)
	})
	route.Put(r, op("/admin/downloads", "saveDownloadSettings", "admin"), func(ctx context.Context, in route.Input[DownloadSettingsDTO, route.Empty, route.Empty]) (route.Output[DownloadSettingsDTO], error) {
		b, _ := json.Marshal(in.Body)
		return out(in.Body, s.DB.PatchSetting(ctx, store.PatchSettingParams{Key: "downloads", Value: b}))
	})
	route.Get(r, op("/audio/musicbrainz", "searchMusicBrainz", "user"), func(ctx context.Context, in route.Input[route.Empty, MusicSearchQuery, route.Empty]) (route.Output[MusicMatchesDTO], error) {
		v, e := searchMusicBrainz(ctx, in.Query.Search)
		return out(v, e)
	})
}

var musicMu sync.Mutex
var musicLast time.Time

func searchMusicBrainz(ctx context.Context, query string) (MusicMatchesDTO, error) {
	v := MusicMatchesDTO{Items: []MusicMatchDTO{}}
	musicMu.Lock()
	defer musicMu.Unlock()
	delay := time.Until(musicLast.Add(time.Second))
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-timer.C:
		}
	}
	musicLast = time.Now()
	req, e := http.NewRequestWithContext(ctx, "GET", "https://musicbrainz.org/ws/2/release/?fmt=json&limit=10&query="+url.QueryEscape(query), nil)
	if e != nil {
		return v, e
	}
	req.Header.Set("User-Agent", "JFE/0.2 (personal media server)")
	resp, e := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if e != nil {
		return v, route.Fail(503, "MusicBrainz unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return v, route.Fail(503, "MusicBrainz unavailable")
	}
	var data struct {
		Releases []struct {
			ID, Title, Date, Country string
			ArtistCredit             []struct{ Name string } `json:"artist-credit"`
		} `json:"releases"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&data); e != nil {
		return v, e
	}
	for _, m := range data.Releases {
		artists := []string{}
		for _, a := range m.ArtistCredit {
			artists = append(artists, a.Name)
		}
		v.Items = append(v.Items, MusicMatchDTO{m.ID, m.Title, strings.Join(artists, ", "), m.Date, m.Country})
	}
	return v, nil
}

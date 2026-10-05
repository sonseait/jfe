package worker

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"io"
	"io/fs"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/settings"
	"jfe/backend/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func stable(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00"))).String()
}

type ScanOptions struct {
	ForceMetadata bool `json:"forceMetadata"`
}

func (w *Worker) scan(ctx context.Context, id string, options ScanOptions, report func(int, int) error) error {
	l, e := w.DB.GetLibrary(ctx, id)
	if e != nil {
		return e
	}
	if audio.IsLibrary(l.Kind) {
		return w.scanAudio(ctx, l, report)
	}
	seen := map[string]bool{}
	roots := map[string]string{}
	paths := []string{}
	updatedShows := map[string]bool{}
	if e = report(0, 0); e != nil {
		return e
	}
	// Only reconcile missing files after every configured root was walked successfully.
	for _, root := range l.Paths {
		root, e = media.Within(w.Config.MediaRoot, root)
		if e != nil {
			return e
		}
		stat, e := os.Stat(root)
		if e != nil || !stat.IsDir() {
			return fmt.Errorf("library root is unavailable")
		}
		e = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() {
				if path != root && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".jfe-") || !media.IsVideo(path) {
				return nil
			}
			real, e := media.Within(root, path)
			if e != nil {
				return e
			}
			if !seen[real] {
				seen[real] = true
				roots[real] = root
				paths = append(paths, real)
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	if e = report(0, len(paths)); e != nil {
		return e
	}
	for n, real := range paths {
		if e = ctx.Err(); e != nil {
			return e
		}
		e = func() error {
			unlock, err := audio.Lock(ctx, w.Config.CacheRoot, real)
			if err != nil {
				return err
			}
			defer unlock()
			stat, e := os.Stat(real)
			if e != nil {
				return e
			}
			old, e := w.DB.FileByPath(ctx, store.FileByPathParams{Path: real, LibraryID: id})
			var probe media.Probe
			cached := e == nil && old.Size == stat.Size() && old.ModifiedAt == stat.ModTime().UnixNano() && json.Unmarshal(old.Probe, &probe) == nil && probe.Version >= media.ProbeVersion
			if cached {
				// Rebuild sidecars each scan even when the video itself has not changed.
				streams := probe.Streams[:0]
				for _, stream := range probe.Streams {
					if stream.ExternalPath == "" {
						streams = append(streams, stream)
					}
				}
				probe.Streams = streams
				e = nil
			} else {
				probe, e = media.Inspect(ctx, real)
			}
			if e != nil {
				return e
			}
			if probe.Duration() <= 0 {
				return fmt.Errorf("media has no duration")
			}
			name := media.Parse(real)
			kind, parentID := "movie", ""
			itemID := stable(id, real)
			showRoot := ""
			if l.Kind == "series" {
				name, showRoot = media.ParseSeries(roots[real], real)
				if name.Series == "" || (showRoot == "" && name.Episode == 0) {
					return nil
				}
				kind = "episode"
				parentID = stable(id, "series", strings.ToLower(name.Series))
				if showRoot != "" {
					parentID = stable(id, "series-folder", showRoot)
				}
				if e = w.DB.UpsertItem(ctx, store.UpsertItemParams{ID: parentID, LibraryID: id, Kind: "series", Title: name.Series, SortTitle: strings.ToLower(name.Series), Year: int32(name.Year)}); e != nil {
					return e
				}
			}
			if e = w.DB.UpsertItem(ctx, store.UpsertItemParams{ID: itemID, LibraryID: id, ParentID: parentID, Kind: kind, Title: name.Title, SortTitle: strings.ToLower(name.Title), Year: int32(name.Year), Season: int32(name.Season), Episode: int32(name.Episode)}); e != nil {
				return e
			}

			if e = w.discoverSubtitleSidecars(real, &probe); e != nil {
				return e
			}
			w.prepareTextSubtitles(ctx, stable("file", id, real), real, &probe, cached)
			b, _ := json.Marshal(probe)
			if e = w.DB.SaveFile(ctx, store.SaveFileParams{ID: stable("file", id, real), ItemID: itemID, Path: real, Size: stat.Size(), ModifiedAt: stat.ModTime().UnixNano(), Duration: probe.Duration(), Probe: b}); e != nil {
				return e
			}
			if parentID != "" && !updatedShows[parentID] {
				if showRoot == "" {
					showRoot = filepath.Dir(real)
				}
				if err := w.localMetadata(ctx, parentID, filepath.Join(showRoot, "tvshow.mkv"), options.ForceMetadata); err != nil {
					return err
				}
				updatedShows[parentID] = true
			}
			return w.localMetadata(ctx, itemID, real, options.ForceMetadata)
		}()
		if e != nil {
			// A corrupt or unsupported file must not prevent the rest of a library from scanning.
			log.Warn().Err(e).Str("file", filepath.Base(real)).Str("libraryId", id).Msg("skipping media file")
			if e = report(n+1, len(paths)); e != nil {
				return e
			}
			continue
		}
		if e = report(n+1, len(paths)); e != nil {
			return e
		}
	}

	files, e := w.DB.LibraryFiles(ctx, id)
	if e != nil {
		return e
	}
	for _, f := range files {
		if !seen[f.Path] {
			if e = w.DB.UnavailableFile(ctx, f.ID); e != nil {
				return e
			}
		}
	}
	if e = w.DB.DeleteEmptySeries(ctx, id); e != nil {
		return e
	}
	return w.DB.ScannedLibrary(ctx, id)
}

type nfo struct {
	Actors []struct {
		Name  string `xml:"name"`
		Role  string `xml:"role"`
		TMDB  int    `xml:"tmdbid"`
		Thumb string `xml:"thumb"`
	} `xml:"actor"`
	Title string `xml:"title"`
	Year  int    `xml:"year"`
	Plot  string `xml:"plot"`
	TMDB  string `xml:"tmdbid"`
}

func (w *Worker) localMetadata(ctx context.Context, id, path string, force bool) error {
	i, e := w.DB.GetItem(ctx, id)
	if e != nil || i.MetadataLocked {
		return e
	}
	n := nfo{}
	for _, p := range []string{strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo", filepath.Join(filepath.Dir(path), "movie.nfo")} {
		if safe, e := media.Within(w.Config.MediaRoot, p); e == nil {
			f, e := os.Open(safe)
			if e == nil {
				e = xml.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&n)
				f.Close()
				if e == nil {
					break
				}
			}
		}
	}
	if n.Title != "" {
		i.Title = n.Title
	}
	if n.Year > 0 {
		i.Year = int32(n.Year)
	}
	if n.Plot != "" {
		i.Overview = n.Plot
	}
	if n.TMDB != "" {
		i.ProviderID = n.TMDB
	}
	if n.Actors != nil {
		cast := []media.CastMember{}
		var previous []media.CastMember
		_ = json.Unmarshal(i.CastMembers, &previous)
		for _, actor := range n.Actors {
			if strings.TrimSpace(actor.Name) != "" {
				person := media.NewCastMember(actor.Name, actor.Role, actor.TMDB)
				for _, old := range previous {
					if old.ID == person.ID {
						person.Image = old.Image
						break
					}
				}
				if actor.Thumb != "" {
					thumb := actor.Thumb
					if !filepath.IsAbs(thumb) {
						thumb = filepath.Join(filepath.Dir(path), thumb)
					}
					if safe, err := media.Within(w.Config.MediaRoot, thumb); err == nil {
						if input, err := os.Open(safe); err == nil {
							if err = w.savePortrait(person.ID, input); err == nil {
								person.Image = "cast-" + person.ID + ".jpg"
							}
							input.Close()
						}
					}
				}
				cast = append(cast, person)
			}
		}
		i.CastMembers, _ = json.Marshal(cast)
	}
	for _, candidate := range []string{strings.TrimSuffix(path, filepath.Ext(path)) + "-poster.jpg", filepath.Join(filepath.Dir(path), "poster.jpg"), filepath.Join(filepath.Dir(path), "folder.jpg")} {
		safe, e := media.Within(w.Config.MediaRoot, candidate)
		if e != nil {
			continue
		}
		input, e := os.Open(safe)
		if e != nil {
			continue
		}
		e = w.saveArtwork(id, input)
		input.Close()
		if e == nil {
			i.Poster = id + ".jpg"
			break
		}
	}
	_, e = w.DB.SaveMetadata(ctx, store.SaveMetadataParams{CastMembers: i.CastMembers, ID: i.ID, Title: i.Title, Year: i.Year, Overview: i.Overview, Poster: i.Poster, ProviderID: i.ProviderID, MetadataLocked: i.MetadataLocked})
	if e != nil {
		return e
	}
	if w.Config.TMDBToken != "" && (i.Kind == "movie" || i.Kind == "series") && (force || i.ProviderID == "") {
		general, err := settings.Load(ctx, w.DB)
		if err != nil {
			return err
		}
		if !force && !general.AutoMetadata {
			return nil
		}
		payload, _ := json.Marshal(struct {
			Automatic bool `json:"automatic"`
		}{!force})
		_, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "metadata", ResourceID: id, Payload: payload})
	}
	return e
}
func (w *Worker) saveArtwork(id string, r io.Reader) error {
	dir := filepath.Join(w.Config.CacheRoot, "artwork")
	if e := os.MkdirAll(dir, 0750); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".art-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = io.Copy(f, io.LimitReader(r, 16<<20))
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, id+".jpg"))
}
func (w *Worker) metadata(ctx context.Context, j store.Job) error {
	if w.Config.TMDBToken == "" {
		return media.ErrTMDBNotConfigured
	}
	i, e := w.DB.GetItem(ctx, j.ResourceID)
	if e != nil {
		return e
	}
	var payload struct {
		ProviderID int    `json:"providerId"`
		Automatic  bool   `json:"automatic"`
		Mode       string `json:"mode"`
	}
	if e = json.Unmarshal(j.Payload, &payload); e != nil {
		return e
	}
	general, e := settings.Load(ctx, w.DB)
	if e != nil {
		return e
	}
	if payload.Automatic && !general.AutoMetadata {
		return nil
	}
	if payload.Mode == "missing" {
		if i.Kind == "series" {
			if i.ProviderID == "" {
				return fmt.Errorf("identify the series before its episodes")
			}
			return w.scheduleEpisodeMetadata(ctx, i.ID, payload.Automatic, payload.Mode)
		}
		// Recheck at execution time: another job may have populated this episode.
		if i.ProviderID != "" {
			return nil
		}
	}
	if i.MetadataLocked && payload.ProviderID == 0 {
		return nil
	}
	kind := "movie"
	metadataID := payload.ProviderID
	path := ""
	if i.Kind == "episode" {
		parent, err := w.DB.GetItem(ctx, i.ParentID)
		if err != nil {
			return err
		}
		if parent.Kind != "series" {
			return fmt.Errorf("episode parent is not a series")
		}
		metadataID, _ = strconv.Atoi(parent.ProviderID)
		if metadataID == 0 {
			return fmt.Errorf("identify the series before its episodes")
		}
		path = "/tv/" + strconv.Itoa(metadataID) + "/season/" + strconv.Itoa(int(i.Season)) + "/episode/" + strconv.Itoa(int(i.Episode))
	} else {
		if i.Kind == "series" {
			kind = "tv"
		}
		if metadataID == 0 && i.ProviderID != "" {
			metadataID, _ = strconv.Atoi(i.ProviderID)
		}
		if metadataID == 0 {
			matches, _, _, err := media.SearchMetadata(ctx, w.Config.TMDBToken, i.Title, kind, int(i.Year), general.MetadataLanguage)
			if err != nil {
				return err
			}
			if len(matches) == 0 || !matches[0].Recommended {
				return media.ErrNeedsIdentification
			}
			metadataID = matches[0].ID
		}
		path = "/" + kind + "/" + strconv.Itoa(metadataID)
	}

	var info struct {
		Credits struct {
			Cast []struct {
				ID        int    `json:"id"`
				Name      string `json:"name"`
				Character string `json:"character"`
				Profile   string `json:"profile_path"`
			} `json:"cast"`
		} `json:"credits"`
		Title    string `json:"title"`
		Name     string `json:"name"`
		Overview string `json:"overview"`
		Poster   string `json:"poster_path"`
		Still    string `json:"still_path"`
		Date     string `json:"release_date"`
		Air      string `json:"first_air_date"`
		AirDate  string `json:"air_date"`
		ID       int    `json:"id"`
	}
	if e = media.TMDB(ctx, w.Config.TMDBToken, path+"?append_to_response=credits&language="+general.MetadataLanguage, &info); e != nil {
		return e
	}
	if i.Kind == "episode" && general.MetadataLanguage != "en-US" && genericEpisodeTitle(info.Name, i.Episode) {
		var english struct {
			Name string `json:"name"`
		}
		if e = media.TMDB(ctx, w.Config.TMDBToken, path+"?language=en-US", &english); e != nil {
			return e
		}
		if english.Name != "" {
			info.Name = english.Name
		}
	}
	cast := []media.CastMember{}
	var oldCast []media.CastMember
	_ = json.Unmarshal(i.CastMembers, &oldCast)
	for _, actor := range info.Credits.Cast {
		if actor.ID > 0 && strings.TrimSpace(actor.Name) != "" {
			person := media.NewCastMember(actor.Name, actor.Character, actor.ID)
			for _, old := range oldCast {
				if old.ID == person.ID {
					person.Image = old.Image
					break
				}
			}
			if general.CastImages {
				if image, err := w.castPortrait(ctx, person.ID, actor.Profile); err != nil {
					return err
				} else {
					person.Image = image
				}
			}
			cast = append(cast, person)
		}
	}
	i.CastMembers, _ = json.Marshal(cast)
	if info.Title == "" {
		info.Title = info.Name
		info.Date = info.Air
	}
	if i.Kind == "episode" {
		info.Date = info.AirDate
	}
	if len(info.Date) >= 4 {
		y, _ := strconv.Atoi(info.Date[:4])
		i.Year = int32(y)
	}
	artwork := info.Poster
	if i.Kind == "episode" && info.Still != "" {
		artwork = info.Still
	}
	if artwork != "" && strings.HasPrefix(artwork, "/") && !strings.Contains(artwork, "..") {
		req, e := http.NewRequestWithContext(ctx, "GET", "https://image.tmdb.org/t/p/w500"+artwork, nil)
		if e != nil {
			return e
		}
		client := http.Client{Timeout: 20 * time.Second}
		res, e := client.Do(req)
		if e != nil {
			return e
		}
		if res.StatusCode == 200 {
			e = w.saveArtwork(i.ID, res.Body)
			if e == nil {
				i.Poster = i.ID + ".jpg"
			}
		}
		res.Body.Close()
		if e != nil {
			return e
		}
	}
	if info.ID == 0 {
		info.ID = metadataID
	}
	_, e = w.DB.SaveMetadata(ctx, store.SaveMetadataParams{CastMembers: i.CastMembers, ID: i.ID, Title: info.Title, Year: i.Year, Overview: info.Overview, Poster: i.Poster, ProviderID: strconv.Itoa(info.ID), MetadataLocked: i.MetadataLocked})
	if e != nil || i.Kind != "series" {
		return e
	}
	return w.scheduleEpisodeMetadata(ctx, i.ID, payload.Automatic, payload.Mode)
}

func (w *Worker) scheduleEpisodeMetadata(ctx context.Context, seriesID string, automatic bool, mode string) error {
	episodes, e := w.DB.SeriesEpisodes(ctx, seriesID)
	if e != nil {
		return e
	}
	episodePayload, _ := json.Marshal(struct {
		Automatic bool   `json:"automatic"`
		Mode      string `json:"mode,omitempty"`
	}{automatic, mode})
	for _, episode := range episodes {
		if episode.MetadataLocked || (mode == "missing" && episode.ProviderID != "") {
			continue
		}
		if _, e = w.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "scanner", Kind: "metadata", ResourceID: episode.ID, Payload: episodePayload}); e != nil {
			return e
		}
	}
	return nil
}

func genericEpisodeTitle(title string, episode int32) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	number := strconv.Itoa(int(episode))
	return title == "episode "+number || title == "tap "+number || title == "tập "+number
}

package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/store"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func (w *Worker) scanAudio(ctx context.Context, l store.Library, report func(int, int) error) error {
	roots := append([]string{}, l.Paths...)
	if w.Config.ImportRoot != "" {
		if st, e := os.Stat(w.Config.ImportRoot); e != nil || !st.IsDir() {
			return fmt.Errorf("import root unavailable")
		}
		imported := filepath.Join(w.Config.ImportRoot, l.ID)
		if st, e := os.Stat(imported); e == nil && st.IsDir() {
			roots = append(roots, imported)
		}
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, root := range roots {
		safe, e := audio.Within(w.Config.MediaRoot, w.Config.ImportRoot, root)
		if e != nil {
			return e
		}
		e = filepath.WalkDir(safe, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() {
				if path != safe && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !audio.IsAudio(path) {
				return nil
			}
			real, e := media.Within(safe, path)
			if e != nil {
				return e
			}
			if !seen[real] {
				paths = append(paths, real)
				seen[real] = true
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	if e := report(0, len(paths)); e != nil {
		return e
	}
	for n, path := range paths {
		unlock, e := audio.Lock(ctx, w.Config.CacheRoot, path)
		if e != nil {
			return e
		}
		e = w.indexAudio(ctx, l, path)
		unlock()
		if e != nil {
			return e
		}
		if e = report(n+1, len(paths)); e != nil {
			return e
		}
	}
	files, e := w.DB.LibraryFiles(ctx, l.ID)
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
	if e = w.indexImportCollections(ctx, l); e != nil {
		return e
	}
	return w.DB.ScannedLibrary(ctx, l.ID)
}
func (w *Worker) indexAudio(ctx context.Context, l store.Library, path string) error {
	probe, e := media.Inspect(ctx, path)
	if e != nil {
		return e
	}
	if probe.Duration() <= 0 {
		return fmt.Errorf("audio has no duration")
	}
	r := audio.ReadResult{}
	if audio.Supported(path) {
		r, e = audio.Helper(ctx, w.Config.Python, "read", path, nil)
		if e != nil {
			return e
		}
	}
	rawTags := r.Tags
	if rawTags.Artists == nil {
		rawTags.Artists = []string{}
	}
	if rawTags.AlbumArtists == nil {
		rawTags.AlbumArtists = []string{}
	}
	if rawTags.Genres == nil {
		rawTags.Genres = []string{}
	}
	// Import metadata is a fallback only; edited embedded tags always take precedence.
	var fallback audio.Tags
	if b, err := os.ReadFile(path + ".json"); err == nil {
		_ = json.Unmarshal(b, &fallback)
	}
	if r.Tags.Title == "" {
		r.Tags.Title = fallback.Title
	}
	if r.Tags.Album == "" {
		r.Tags.Album = fallback.Album
	}
	if len(r.Tags.Artists) == 0 {
		r.Tags.Artists = fallback.Artists
	}
	if r.Tags.Title == "" {
		r.Tags.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if r.Tags.Album == "" {
		r.Tags.Album = filepath.Base(filepath.Dir(path))
	}
	groupKind, kind := "album", "track"
	if l.Kind == "podcasts" {
		groupKind, kind = "podcast", "podcast_episode"
	}
	if l.Kind == "audiobooks" {
		groupKind, kind = "audiobook", "book_part"
	}
	// Folder identity is stable across tag edits and prevents accidental album merges.
	groupDir := filepath.Dir(path)
	if _, err := os.Stat(filepath.Join(groupDir, ".ready")); err == nil {
		groupDir = filepath.Dir(groupDir)
	}
	if discFolder.MatchString(filepath.Base(groupDir)) {
		groupDir = filepath.Dir(groupDir)
	}
	parent := stable(l.ID, "audio-folder", groupDir)
	id := stable(l.ID, path)
	year, _ := strconv.Atoi(strings.Split(r.Tags.Date, "-")[0])
	track, _ := strconv.Atoi(strings.Split(r.Tags.Track, "/")[0])
	disc, _ := strconv.Atoi(strings.Split(r.Tags.Disc, "/")[0])
	if track == 0 {
		track, _ = strconv.Atoi(strings.Split(fallback.Track, "/")[0])
	}
	for _, item := range []store.UpsertItemParams{{ID: parent, LibraryID: l.ID, Kind: groupKind, Title: r.Tags.Album, SortTitle: strings.ToLower(r.Tags.Album)}, {ID: id, LibraryID: l.ID, ParentID: parent, Kind: kind, Title: r.Tags.Title, SortTitle: strings.ToLower(r.Tags.Title), Year: int32(year), Season: int32(disc), Episode: int32(track)}} {
		if e = w.DB.UpsertItem(ctx, item); e != nil {
			return e
		}
	}
	poster := ""
	if r.Artwork != "" {
		data, err := base64.StdEncoding.DecodeString(r.Artwork)
		if err == nil && len(data) <= 10<<20 {
			dir := filepath.Join(w.Config.CacheRoot, "artwork")
			if e = os.MkdirAll(dir, 0750); e != nil {
				return e
			}
			poster = id + ".jpg"
			if http.DetectContentType(data) == "image/png" {
				poster = id + ".png"
			}
			if e = os.WriteFile(filepath.Join(dir, poster), data, 0640); e != nil {
				return e
			}
		}
	} else {
		for _, name := range []string{"cover.jpg", "folder.jpg", "cover.png"} {
			safe, err := media.Within(filepath.Dir(path), filepath.Join(filepath.Dir(path), name))
			if err != nil {
				continue
			}
			st, err := os.Stat(safe)
			if err != nil || st.Size() > 10<<20 {
				continue
			}
			data, err := os.ReadFile(safe)
			if err != nil {
				continue
			}
			dir := filepath.Join(w.Config.CacheRoot, "artwork")
			if e = os.MkdirAll(dir, 0750); e != nil {
				return e
			}
			poster = id + ".jpg"
			if http.DetectContentType(data) == "image/png" {
				poster = id + ".png"
			}
			if e = os.WriteFile(filepath.Join(dir, poster), data, 0640); e != nil {
				return e
			}
			break
		}
	}
	if e = w.DB.UpdateAudioTitle(ctx, store.UpdateAudioTitleParams{ID: id, Title: r.Tags.Title, Year: int32(year), Poster: poster}); e != nil {
		return e
	}
	groupPoster := poster
	if groupPoster == "" {
		if children, err := w.DB.AudioChildren(ctx, parent); err == nil {
			for _, child := range children {
				if child.Poster != "" {
					groupPoster = child.Poster
					break
				}
			}
		}
	}
	if e = w.DB.UpdateAudioTitle(ctx, store.UpdateAudioTitleParams{ID: parent, Title: r.Tags.Album, Year: int32(year), Poster: groupPoster}); e != nil {
		return e
	}
	if len(probe.Chapters) == 0 {
		if b, err := os.ReadFile(path + ".chapters.json"); err == nil {
			_ = json.Unmarshal(b, &probe.Chapters)
		}
	}
	tags, _ := json.Marshal(rawTags)
	chapters, _ := json.Marshal(probe.Chapters)
	if string(chapters) == "null" {
		chapters = []byte("[]")
	}
	if e = w.DB.SaveAudioMetadata(ctx, store.SaveAudioMetadataParams{ItemID: id, Tags: tags, Chapters: chapters}); e != nil {
		return e
	}
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(probe)
	return w.DB.SaveFile(ctx, store.SaveFileParams{ID: stable("file", l.ID, path), ItemID: id, Path: path, Size: st.Size(), ModifiedAt: st.ModTime().UnixNano(), Duration: probe.Duration(), Probe: b})
}
func (w *Worker) validLease(ctx context.Context, j store.Job) error {
	ok, e := w.DB.ValidJobLease(ctx, store.ValidJobLeaseParams{ID: j.ID, LeaseID: j.LeaseID})
	if e != nil {
		return e
	}
	if !ok {
		return context.Canceled
	}
	return nil
}
func (w *Worker) importPermission(ctx context.Context, user, library string) error {
	ok, e := w.DB.CanImport(ctx, store.CanImportParams{LibraryID: library, UserID: user})
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("audio permission revoked")
	}
	return nil
}
func audioDigest(ctx context.Context, path string) (string, error) {
	b, e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-map", "0:a", "-c", "copy", "-f", "hash", "-hash", "sha256", "-").Output()
	if e != nil {
		return "", fmt.Errorf("cannot verify audio payload")
	}
	return string(b), nil
}
func (w *Worker) writeAudioTags(ctx context.Context, j store.Job) error {
	t, e := w.DB.GetTagJob(ctx, j.ID)
	if e != nil {
		return e
	}
	f, e := w.DB.GetFile(ctx, t.FileID)
	if e != nil {
		return e
	}
	i, e := w.DB.GetItem(ctx, f.ItemID)
	if e != nil {
		return e
	}
	if e = w.importPermission(ctx, t.UserID, i.LibraryID); e != nil {
		return e
	}
	path, e := audio.Within(w.Config.MediaRoot, w.Config.ImportRoot, f.Path)
	if e != nil {
		return e
	}
	unlock, e := audio.Lock(ctx, w.Config.CacheRoot, path)
	if e != nil {
		return e
	}
	defer unlock()
	var patch audio.Patch
	if e = json.Unmarshal(t.Patch, &patch); e != nil {
		return e
	}
	// A durable journal distinguishes recovery after rename from an unrelated edit.
	journal := filepath.Join(filepath.Dir(path), ".jfe-tag-"+j.ID+".json")
	tmp := filepath.Join(filepath.Dir(path), ".jfe-tag-"+j.ID+filepath.Ext(path))
	defer os.Remove(tmp)
	if t.Phase == "indexed" {
		return nil
	}
	current, e := audio.Fingerprint(path)
	if e != nil {
		return e
	}
	prepared, _ := os.ReadFile(journal)
	published := len(prepared) > 0 && current == string(prepared)
	if !published {
		if current != t.Fingerprint {
			return errors.New("file changed; reload tags before retrying")
		}
		if e = audio.Writable(path); e != nil {
			return e
		}
		st, e := os.Stat(path)
		if e != nil {
			return e
		}
		src, e := os.Open(path)
		if e != nil {
			return e
		}
		defer src.Close()
		dst, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
		if e != nil {
			return e
		}
		_, e = audio.CopyContext(ctx, dst, src)
		if e == nil {
			e = dst.Sync()
		}
		closeErr := dst.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		original, e := audioDigest(ctx, path)
		if e != nil {
			return e
		}
		if _, e = audio.Helper(ctx, w.Config.Python, "write", tmp, &patch); e != nil {
			return e
		}
		updated, e := audioDigest(ctx, tmp)
		if e != nil {
			return e
		}
		if original != updated {
			return errors.New("audio payload changed during tagging")
		}
		before, e := media.Inspect(ctx, path)
		if e != nil {
			return e
		}
		after, e := media.Inspect(ctx, tmp)
		if e != nil {
			return e
		}
		a, _ := json.Marshal(before.Chapters)
		b, _ := json.Marshal(after.Chapters)
		if string(a) != string(b) {
			return errors.New("chapters changed during tagging")
		}
		// Fingerprint includes the path, so compute the post-rename identity explicitly.
		if e = audio.PreserveMode(path, tmp); e != nil {
			return e
		}
		if e = audio.SyncFile(tmp); e != nil {
			return e
		}
		targetPrint, e := audio.FingerprintAt(tmp, path)
		if e != nil {
			return e
		}
		if e = audio.WriteJournal(journal, []byte(targetPrint)); e != nil {
			return e
		}
		if e = w.DB.SetTagPhase(ctx, store.SetTagPhaseParams{ID: j.ID, Phase: "prepared"}); e != nil {
			return e
		}
		if e = w.validLease(ctx, j); e != nil {
			return e
		}
		if e = w.importPermission(ctx, t.UserID, i.LibraryID); e != nil {
			return e
		}
		check, e := audio.Fingerprint(path)
		if e != nil {
			return e
		}
		if check != current {
			return errors.New("file changed during tagging")
		}
		if e = os.Rename(tmp, path); e != nil {
			return e
		}
		if e = audio.SyncDirectory(filepath.Dir(path)); e != nil {
			return e
		}
	}
	l, e := w.DB.GetLibrary(ctx, i.LibraryID)
	if e != nil {
		return e
	}
	if e = w.indexAudio(ctx, l, path); e != nil {
		return e
	}
	if e = w.DB.SetTagPhase(ctx, store.SetTagPhaseParams{ID: j.ID, Phase: "indexed"}); e != nil {
		return e
	}
	_ = os.Remove(journal)
	return nil
}

var discFolder = regexp.MustCompile(`(?i)^(?:disc|disk|cd)[ ._-]*[0-9]+$`)

func (w *Worker) indexImportCollections(ctx context.Context, l store.Library) error {
	if w.Config.ImportRoot == "" {
		return nil
	}
	sources, e := w.DB.LibraryImportSources(ctx, l.ID)
	if e != nil {
		return e
	}
	root, e := filepath.EvalSymlinks(w.Config.ImportRoot)
	if e != nil {
		return e
	}
	kind := "album"
	if l.Kind == "podcasts" {
		kind = "podcast"
	}
	if l.Kind == "audiobooks" {
		kind = "audiobook"
	}
	for _, source := range sources {
		entries, e := w.DB.ListImportEntries(ctx, source.ID)
		if e != nil {
			return e
		}
		group := stable(l.ID, "audio-folder", filepath.Join(root, l.ID, source.ID))
		created := false
		for _, entry := range entries {
			if entry.State != "ready" {
				continue
			}
			imported, e := w.DB.GetImportedFile(ctx, store.GetImportedFileParams{LibraryID: l.ID, VideoID: entry.VideoID})
			if e != nil {
				continue
			}
			file, e := w.DB.FileByPath(ctx, store.FileByPathParams{Path: imported.Path, LibraryID: l.ID})
			if e != nil {
				continue
			}
			if !created {
				if e = w.DB.UpsertItem(ctx, store.UpsertItemParams{ID: group, LibraryID: l.ID, Kind: kind, Title: source.Title, SortTitle: strings.ToLower(source.Title)}); e != nil {
					return e
				}
				created = true
			}
			if e = w.DB.SaveAudioMember(ctx, store.SaveAudioMemberParams{CollectionID: group, ItemID: file.ItemID, Ordinal: entry.Ordinal}); e != nil {
				return e
			}
		}
	}
	return nil
}

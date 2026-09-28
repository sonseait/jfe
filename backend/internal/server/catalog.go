package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/media"
	"jfe/backend/internal/route"
	"jfe/backend/internal/settings"
	"jfe/backend/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) libraryDTO(ctx context.Context, l store.Library) (LibraryDTO, error) {
	d := LibraryDTO{ID: l.ID, Name: l.Name, Kind: l.Kind, Paths: l.Paths, ScanIntervalHours: int(l.ScanIntervalHours), LastScanAt: l.LastScanAt}
	var err error
	d.FileCount, err = s.DB.LibraryFileCount(ctx, l.ID)
	if err != nil {
		return d, err
	}
	scan, err := s.DB.LatestLibraryScan(ctx, l.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	job := jobDTO(scan)
	d.Scan = &job
	return d, nil
}
func jobDTO(j store.Job) JobDTO {
	return JobDTO{Role: j.Role, Attempts: int(j.Attempts), CancelRequested: j.CancelRequested, UpdatedAt: j.UpdatedAt, ID: j.ID, Kind: j.Kind, ResourceID: j.ResourceID, State: j.State, Progress: int(j.Progress), Error: j.Error, CreatedAt: j.CreatedAt, TotalFiles: int(j.TotalFiles), ProcessedFiles: int(j.ProcessedFiles)}
}
func (s *Server) enqueue(ctx context.Context, role, kind, id string, payload any) (JobDTO, error) {
	b := []byte("{}")
	if payload != nil {
		b, _ = json.Marshal(payload)
	}
	j, e := s.DB.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: role, Kind: kind, ResourceID: id, Payload: b})
	return jobDTO(j), e
}
func (s *Server) saveLibrary(ctx context.Context, id string, b LibraryRequest) (LibraryDTO, error) {
	if id == "" {
		id = uuid.NewString()
	} else {
		old, e := s.DB.GetLibrary(ctx, id)
		if e != nil {
			return LibraryDTO{}, e
		}
		if old.Kind != b.Kind {
			return LibraryDTO{}, route.Fail(422, "Library kind cannot change")
		}
	}
	if len(b.Paths) == 0 && !audio.IsLibrary(b.Kind) {
		return LibraryDTO{}, route.Fail(422, "Video libraries require a media path")
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, path := range b.Paths {
		p, e := media.Within(s.Config.MediaRoot, path)
		if e != nil {
			return LibraryDTO{}, route.Fail(422, "Media path unavailable or outside configured root")
		}
		info, e := os.Stat(p)
		if e != nil || !info.IsDir() {
			return LibraryDTO{}, route.Fail(422, "Media path must be an available directory")
		}
		if !seen[p] {
			paths = append(paths, p)
			seen[p] = true
		}
	}
	l, e := s.DB.SaveLibrary(ctx, store.SaveLibraryParams{ID: id, Name: strings.TrimSpace(b.Name), Kind: b.Kind, Paths: paths, ScanIntervalHours: int32(b.ScanIntervalHours)})
	if e != nil {
		return LibraryDTO{}, e
	}
	return s.libraryDTO(ctx, l)
}
func (s *Server) authorizedItem(ctx context.Context, id string) (store.Item, error) {
	i, e := s.DB.GetItem(ctx, id)
	if e == nil {
		e = s.access(ctx, i.LibraryID)
	}
	return i, e
}
func (s *Server) itemDTO(ctx context.Context, i store.Item) ItemDTO {
	st, _ := s.DB.GetState(ctx, store.GetStateParams{UserID: route.User(ctx).ID, ItemID: i.ID})
	poster := ""
	if i.Poster != "" {
		poster = "/api/v1/items/" + i.ID + "/image?token="
	}
	return ItemDTO{i.ID, i.LibraryID, i.ParentID, i.Kind, i.Title, int(i.Year), int(i.Season), int(i.Episode), i.Overview, poster, i.ProviderID, i.MetadataLocked, st.Favorite, st.Watched, st.Position}
}

type cursor struct{ Title, ID, Fingerprint string }

func (s *Server) catalog(ctx context.Context, q CatalogQuery) (ItemsDTO, error) {
	v := ItemsDTO{Items: []ItemDTO{}}
	p := route.User(ctx)
	rawCursor := q.Cursor
	q.Cursor = ""
	b, _ := json.Marshal(q)
	fingerprint := hash(p.ID + string(b))
	cur := cursor{}
	if rawCursor != "" {
		data, e := base64.RawURLEncoding.DecodeString(rawCursor)
		if e != nil || json.Unmarshal(data, &cur) != nil || cur.Fingerprint != fingerprint {
			return v, route.Fail(422, "Invalid cursor")
		}
	}
	limit := q.Limit
	if limit == 0 {
		limit = 36
	}
	rows, e := s.DB.ListItems(ctx, store.ListItemsParams{Artist: q.Artist, TopLevel: q.TopLevel, IsAdmin: p.Role == "admin", UserID: p.ID, LibraryID: q.LibraryID, ParentID: q.ParentID, Kind: q.Kind, PersonID: q.PersonID, Search: q.Search, Favorites: q.Favorites, Resume: q.Resume, AfterTitle: cur.Title, AfterID: cur.ID, PageLimit: int32(limit + 1)})
	if e != nil {
		return v, e
	}
	v.HasMore = len(rows) > limit
	if v.HasMore {
		rows = rows[:limit]
	}
	for _, i := range rows {
		v.Items = append(v.Items, s.itemDTO(ctx, i))
	}
	if v.HasMore {
		last := rows[len(rows)-1]
		data, _ := json.Marshal(cursor{last.SortTitle, last.ID, fingerprint})
		v.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return v, nil
}
func (s *Server) detail(ctx context.Context, id string) (DetailDTO, error) {
	i, e := s.authorizedItem(ctx, id)
	v := DetailDTO{Files: []FileDTO{}, Cast: []CastDTO{}}
	if e != nil {
		return v, e
	}
	v.Item = s.itemDTO(ctx, i)
	if audio.IsItem(i.Kind) {
		m, err := s.DB.GetAudioMetadata(ctx, i.ID)
		if err == nil {
			d := AudioMetadataDTO{Chapters: []AudioChapterDTO{}}
			_ = json.Unmarshal(m.Tags, &d.Tags)
			var chapters []media.Chapter
			_ = json.Unmarshal(m.Chapters, &chapters)
			for _, c := range chapters {
				start, _ := strconv.ParseFloat(string(c.Start), 64)
				end, _ := strconv.ParseFloat(string(c.End), 64)
				d.Chapters = append(d.Chapters, AudioChapterDTO{c.Tags["title"], start, end})
			}
			v.Audio = &d
		}
	}
	if i.Kind == "album" || i.Kind == "podcast" || i.Kind == "audiobook" {
		children, err := s.DB.AudioChildren(ctx, i.ID)
		if err != nil {
			return v, err
		}
		for _, child := range children {
			v.Children = append(v.Children, s.itemDTO(ctx, child))
		}
	}
	var cast []media.CastMember
	if e = json.Unmarshal(i.CastMembers, &cast); e != nil {
		return v, e
	}
	for _, person := range cast {
		d := CastDTO{ID: person.ID, Name: person.Name, Character: person.Character}
		if person.Image != "" {
			d.Image = "/api/v1/items/" + i.ID + "/cast/" + person.ID + "/image?token="
		}
		v.Cast = append(v.Cast, d)
	}
	files, e := s.DB.ItemFiles(ctx, id)
	if e != nil {
		return v, e
	}
	for _, f := range files {
		var probe media.Probe
		_ = json.Unmarshal(f.Probe, &probe)
		d := FileDTO{ID: f.ID, Name: filepath.Base(f.Path), Size: f.Size, Duration: f.Duration, Available: f.Available, Tracks: []TrackDTO{}}
		for _, t := range probe.Streams {
			if t.Disposition.AttachedPic != 0 {
				continue
			}
			if t.Type == "video" && t.Disposition.AttachedPic == 0 && d.Width == 0 {
				d.Width, d.Height = t.Width, t.Height
			}
			d.Tracks = append(d.Tracks, TrackDTO{t.Index, t.Type, t.Codec, t.Tags["language"], t.Tags["title"]})
		}
		v.Files = append(v.Files, d)
	}
	if i.ParentID != "" {
		all, e := s.DB.ListItems(ctx, store.ListItemsParams{IsAdmin: route.User(ctx).Role == "admin", UserID: route.User(ctx).ID, ParentID: i.ParentID, PageLimit: 10000})
		if e == nil {
			var candidate *store.Item
			for n := range all {
				j := &all[n]
				if j.Season > i.Season || (j.Season == i.Season && j.Episode > i.Episode) {
					if candidate == nil || j.Season < candidate.Season || (j.Season == candidate.Season && j.Episode < candidate.Episode) {
						candidate = j
					}
				}
			}
			if candidate != nil {
				v.NextID = candidate.ID
			}
		}
	}
	return v, nil
}
func (s *Server) searchMetadata(ctx context.Context, q MetadataSearchQuery) (MetadataMatches, error) {
	v := MetadataMatches{Items: []MetadataMatch{}}
	if s.Config.TMDBToken == "" {
		return v, route.Fail(503, "TMDB token is not configured")
	}
	general, err := settings.Load(ctx, s.DB)
	if err != nil {
		return v, err
	}
	matches, title, year, err := media.SearchMetadata(ctx, s.Config.TMDBToken, q.Search, q.Kind, q.Year, general.MetadataLanguage)
	v.Search, v.Year = title, year
	for _, m := range matches {
		v.Items = append(v.Items, MetadataMatch{ID: m.ID, Title: m.Title, Year: m.Date, Overview: m.Overview, Score: m.Score, Recommended: m.Recommended})
	}
	return v, err
}

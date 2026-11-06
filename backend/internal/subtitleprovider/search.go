package subtitleprovider

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"
	"jfe/backend/internal/media"
)

// SearchTarget uses saved movie metadata, or series metadata and episode
// coordinates. TMDB IDs help reject conflicts but are not required for a match.
type SearchTarget struct {
	Title           string
	Aliases         []string
	Year            int
	Kind            string
	TMDBID          int
	Season, Episode int
}

type englishTitle struct {
	Title        string `json:"title"`
	Name         string `json:"name"`
	Original     string `json:"original_title"`
	OriginalName string `json:"original_name"`
	until        time.Time
}

var englishTitles = struct {
	sync.Mutex
	items map[string]englishTitle
}{items: make(map[string]englishTitle)}

// WithFilenameFallback prefers a cleaned Latin release title over localized
// metadata when the English TMDB lookup is unavailable. Keep the metadata name
// as an alias and never replace its year with a conflicting filename year.
func (target SearchTarget) WithFilenameFallback(path string) SearchTarget {
	target.Aliases = append(target.Aliases, media.CleanSearchTitle(target.Title))
	name := media.Parse(path)
	title := name.Title
	if target.Kind == "episode" {
		title = name.Series
	} else {
		if target.Year > 0 && name.Year > 0 && target.Year != name.Year {
			return target
		}
		if target.Year == 0 {
			target.Year = name.Year
		}
	}
	title = media.CleanSearchTitle(title)
	if strings.TrimSpace(target.Title) == "" || strings.ContainsFunc(title, func(r rune) bool { return unicode.Is(unicode.Latin, r) }) {
		if title != "" {
			target.Title = title
		}
	}
	return target
}

// ResolveSearchTarget prefers TMDB's English title without changing saved
// metadata or its release year. Optional lookup failures keep the fallback title.
func ResolveSearchTarget(ctx context.Context, token string, target SearchTarget) SearchTarget {
	target.Title = media.CleanSearchTitle(target.Title)
	target.Aliases = append(target.Aliases, target.Title)
	if token == "" || target.TMDBID <= 0 {
		return target
	}
	kind := "movie"
	if target.Kind == "episode" {
		kind = "tv"
	}
	key := kind + "/" + strconv.Itoa(target.TMDBID)
	englishTitles.Lock()
	info, cached := englishTitles.items[key]
	englishTitles.Unlock()
	if !cached || !info.until.After(time.Now()) {
		info = englishTitle{}
		lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := media.TMDB(lookup, token, "/"+key+"?language=en-US", &info); err != nil {
			event := log.Ctx(ctx).Warn().Str("reason", "tmdb_english_title_lookup_failed")
			var provider *media.ProviderError
			if errors.As(err, &provider) {
				event.Int("httpStatus", provider.Status)
			} else if errors.Is(err, context.DeadlineExceeded) {
				event.Str("lookupFailure", "timeout")
			} else if errors.Is(err, context.Canceled) {
				event.Str("lookupFailure", "cancelled")
			}
			event.Str("subtitleSearchTitle", target.Title).Int("subtitleSearchYear", target.Year).Msg("TMDB English subtitle search title unavailable; using fallback title")
			return target
		}
		info.until = time.Now().Add(6 * time.Hour)
		englishTitles.Lock()
		if len(englishTitles.items) >= 512 {
			clear(englishTitles.items)
		}
		englishTitles.items[key] = info
		englishTitles.Unlock()
	}
	for _, title := range []string{info.Title, info.Name, info.Original, info.OriginalName} {
		if title != "" {
			target.Aliases = append(target.Aliases, media.CleanSearchTitle(title))
		}
	}
	if title := strings.TrimSpace(info.Title); title != "" {
		target.Title = media.CleanSearchTitle(title)
	} else if title := strings.TrimSpace(info.Name); title != "" {
		target.Title = media.CleanSearchTitle(title)
	}
	return target
}

func (target SearchTarget) Query(language, override string) url.Values {
	title, year := target.Title, target.Year
	if strings.TrimSpace(override) != "" {
		title, year = media.MetadataQuery(override, year)
	}
	q := url.Values{"query": {title}, "languages": {language}, "type": {target.Kind}, "order_by": {"download_count"}, "order_direction": {"desc"}}
	if target.Kind == "episode" {
		q.Set("season_number", strconv.Itoa(target.Season))
		q.Set("episode_number", strconv.Itoa(target.Episode))
	} else if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	return q
}

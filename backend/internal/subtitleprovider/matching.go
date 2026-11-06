package subtitleprovider

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"jfe/backend/internal/media"
)

var languageWords = regexp.MustCompile(`[\s._\-\[\]()]+`)

func Vietnamese(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "vietnamese") || strings.Contains(value, "tiếng việt") || strings.Contains(value, "tieng viet") {
		return true
	}
	for _, word := range languageWords.Split(value, -1) {
		if word == "vi" || word == "vie" {
			return true
		}
	}
	return false
}

// RankCandidates accepts matching metadata title/year without requiring provider
// IDs, video hashes, release source or release group. Known ID conflicts are
// rejected; episode matches use the series title and season/episode coordinates.
func RankCandidates(items []Candidate, target SearchTarget) []Candidate {
	accepted := []Candidate{}
	seen := map[int]bool{}
	titles := append([]string{target.Title}, target.Aliases...)
	matchesTitle := func(names ...string) bool {
		for _, title := range titles {
			for _, name := range names {
				if media.SameTitle(title, name) {
					return true
				}
			}
		}
		return false
	}
	for _, c := range items {
		if c.FileID <= 0 || (c.Language != "vi" && c.Language != "vie") {
			continue
		}
		release, file := media.Parse(c.Release), media.Parse(c.Name)
		feature := media.Parse(c.Title + ".mkv")
		identity := false
		titleMatch := false
		if target.Kind == "episode" {
			if target.Episode <= 0 || c.Season != target.Season || c.Episode != target.Episode || (target.TMDBID > 0 && c.ParentTMDBID > 0 && c.ParentTMDBID != target.TMDBID) {
				continue
			}
			identity = target.TMDBID > 0 && c.ParentTMDBID == target.TMDBID
			titleMatch = matchesTitle(c.ParentTitle, release.Series, file.Series)
		} else {
			if c.Episode > 0 || (target.TMDBID > 0 && c.TMDBID > 0 && c.TMDBID != target.TMDBID) {
				continue
			}
			identity = target.TMDBID > 0 && c.TMDBID == target.TMDBID
			year := c.Year
			for _, fallback := range []int{feature.Year, release.Year, file.Year} {
				if year == 0 {
					year = fallback
				}
			}
			if target.Year > 0 {
				if year != target.Year {
					continue
				}
			} else if !identity {
				continue
			}
			titleMatch = matchesTitle(c.Title, feature.Title, release.Title, file.Title)
		}
		if !identity && !titleMatch {
			continue
		}
		accepted = append(accepted, c)
	}
	sort.SliceStable(accepted, func(i, j int) bool {
		a, b := accepted[i], accepted[j]
		if a.HearingImpaired != b.HearingImpaired {
			return !a.HearingImpaired
		}
		if a.Downloads != b.Downloads {
			return a.Downloads > b.Downloads
		}
		return a.FileID < b.FileID
	})
	result := make([]Candidate, 0, len(accepted))
	for _, c := range accepted {
		if !seen[c.FileID] {
			result = append(result, c)
			seen[c.FileID] = true
		}
	}
	return result
}

// RenderSRT uses validated plain-text cues from the existing bounded parser.
func RenderSRT(cues []media.Cue) ([]byte, error) {
	var b strings.Builder
	timestamp := func(value float64) string {
		ms := int64(math.Round(value * 1000))
		return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
	}
	for n, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n+1, timestamp(c.Start), timestamp(c.End), c.Text)
		if b.Len() > textLimit {
			return nil, fmt.Errorf("subtitle exceeds 512 KiB")
		}
	}
	return []byte(b.String()), nil
}

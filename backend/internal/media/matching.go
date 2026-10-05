package media

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var ErrNeedsIdentification = errors.New("Metadata match is ambiguous or missing; identify this item manually")

type Match struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	Overview      string `json:"overview"`
	Date          string `json:"release_date"`
	Air           string `json:"first_air_date"`
	Score         int
	Recommended   bool
}

// SearchMetadata accepts a title or a release filename, retaining the parsed year.
func SearchMetadata(ctx context.Context, token, search, kind string, year int, language string) ([]Match, string, int, error) {
	title, year := metadataQuery(search, year)
	if kind == "series" {
		kind = "tv"
	}
	var result struct {
		Results []Match `json:"results"`
	}
	err := TMDB(ctx, token, "/search/"+kind+"?query="+url.QueryEscape(title)+"&language="+url.QueryEscape(language), &result)
	RankMatches(result.Results, title, year)
	return result.Results, title, year, err
}

func metadataQuery(search string, year int) (string, int) {
	title := strings.TrimSpace(search)
	if IsVideo(title) {
		n := Parse(title)
		title = n.Title
		if n.Series != "" {
			title = n.Series
		}
		if n.Year > 0 {
			year = n.Year
		}
	}
	return title, year
}

func normalizedTitle(title string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(title)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func titleScore(a, b string) int {
	a, b = normalizedTitle(a), normalizedTitle(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 85
	}
	words := map[string]bool{}
	for _, w := range strings.Fields(a) {
		words[w] = true
	}
	other := map[string]bool{}
	for _, w := range strings.Fields(b) {
		other[w] = true
	}
	common := 0
	for w := range words {
		if other[w] {
			common++
		}
	}
	// Fuzzy names rank below exact names; a sole result has its own acceptance threshold.
	return 70 * common / (len(words) + len(other) - common)
}

func RankMatches(matches []Match, title string, year int) {
	for n := range matches {
		m := &matches[n]
		if m.Title == "" {
			m.Title, m.Date = m.Name, m.Air
		}
		m.Score = max(titleScore(title, m.Title), titleScore(title, m.OriginalTitle), titleScore(title, m.OriginalName))
		if year > 0 && len(m.Date) >= 4 {
			y, _ := strconv.Atoi(m.Date[:4])
			if y == year {
				m.Score += 15
			} else if y > 0 {
				m.Score = max(0, m.Score-25)
			}
		}
		m.Recommended = false
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	if len(matches) == 0 {
		return
	}
	if len(matches) == 1 {
		matches[0].Recommended = matches[0].Score > 50
		return
	}
	threshold := 85
	if year > 0 {
		threshold = 100
	}
	if matches[0].Score >= threshold && matches[0].Score-matches[1].Score >= 15 {
		matches[0].Recommended = true
	}
}

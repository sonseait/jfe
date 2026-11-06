package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
	"jfe/backend/internal/media"
	"jfe/backend/internal/recommendations"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type RecommendationRequest struct {
	Focus       string `json:"focus" jsonschema:"enum=mixed,enum=missing,enum=discover"`
	Language    string `json:"language" jsonschema:"enum=en-US,enum=vi-VN"`
	Preferences string `json:"preferences" jsonschema:"maxLength=1000"`
}
type MovieRecommendationDTO struct {
	TMDBID        int      `json:"tmdbId"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"originalTitle"`
	Year          int      `json:"year"`
	Reason        string   `json:"reason"`
	RelatedTitles []string `json:"relatedTitles"`
}
type RecommendationsDTO struct {
	Movies        []MovieRecommendationDTO `json:"movies"`
	CatalogCount  int                      `json:"catalogCount"`
	AnalyzedCount int                      `json:"analyzedCount"`
	GeneratedAt   string                   `json:"generatedAt"`
	Focus         string                   `json:"focus"`
	Language      string                   `json:"language"`
}
type RecommendationStatusDTO struct {
	GeminiConfigured bool                `json:"geminiConfigured"`
	TMDBConfigured   bool                `json:"tmdbConfigured"`
	Last             *RecommendationsDTO `json:"last,omitempty"`
}

const recommendationKey = "movie-recommendations"

func recommendationTitleKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
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

func (s *Server) recommendationRoutes() {
	route.Get(s.Routes, op("/admin/recommendations", "getRecommendations", "admin"), func(ctx context.Context, _ route.Input[route.Empty, route.Empty, route.Empty]) (route.Output[RecommendationStatusDTO], error) {
		v := RecommendationStatusDTO{GeminiConfigured: strings.TrimSpace(s.Config.GeminiKey) != "", TMDBConfigured: s.Config.TMDBToken != ""}
		data, err := s.DB.GetSetting(ctx, recommendationKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out(v, err)
		}
		if err == nil {
			var last RecommendationsDTO
			if json.Unmarshal(data, &last) != nil {
				return out(v, route.Fail(500, "Cannot read saved recommendations"))
			}
			rows, err := s.DB.RecommendationMovies(ctx)
			if err != nil {
				return out(v, err)
			}
			if len(rows) <= 10000 {
				last.Movies = filterOwnedRecommendations(last.Movies, rows)
				v.Last = &last
			}
		}
		return out(v, nil)
	})
	route.Post(s.Routes, op("/admin/recommendations", "generateRecommendations", "admin"), func(ctx context.Context, in route.Input[RecommendationRequest, route.Empty, route.Empty]) (route.Output[RecommendationsDTO], error) {
		if strings.TrimSpace(s.Config.GeminiKey) == "" || s.Config.TMDBToken == "" {
			return out(RecommendationsDTO{}, route.Fail(503, "Gemini and TMDB must be configured"))
		}
		if !s.recommendationsMu.TryLock() {
			return out(RecommendationsDTO{}, route.Fail(429, "Recommendations are already being generated"))
		}
		defer s.recommendationsMu.Unlock()
		v, err := s.generateRecommendations(ctx, in.Body)
		return out(v, err)
	})
}

func (s *Server) generateRecommendations(ctx context.Context, in RecommendationRequest) (RecommendationsDTO, error) {
	ctx, cancel := context.WithTimeout(ctx, 110*time.Second)
	defer cancel()
	result := RecommendationsDTO{Movies: []MovieRecommendationDTO{}, Focus: in.Focus, Language: in.Language}
	rows, err := s.DB.RecommendationMovies(ctx)
	if err != nil {
		return result, err
	}
	if len(rows) == 0 {
		return result, route.Fail(422, "Add movies to the library before requesting recommendations")
	}
	if len(rows) > 10000 {
		return result, route.Fail(422, "Collection exceeds the 10000-title recommendation limit")
	}
	ownedIDs := map[int]bool{}
	ownedTitles := map[string]bool{}
	unique := make([]recommendations.Movie, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		id, _ := strconv.Atoi(row.ProviderID)
		if id > 0 {
			ownedIDs[id] = true
		}
		titleKey := recommendationTitleKey(row.Title) + ":" + strconv.Itoa(int(row.Year))
		ownedTitles[titleKey] = true
		if row.Year == 0 {
			ownedTitles[recommendationTitleKey(row.Title)+":unknown"] = true
		}
		key := titleKey
		if id > 0 {
			key = "tmdb:" + strconv.Itoa(id)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, recommendations.Movie{Title: row.Title, Year: row.Year})
	}
	result.CatalogCount = len(unique)
	// Spread a deterministic bounded sample across the entire sorted collection.
	sample := unique
	if len(sample) > 1000 {
		sample = make([]recommendations.Movie, 1000)
		for n := range sample {
			sample[n] = unique[n*len(unique)/len(sample)]
		}
	}
	result.AnalyzedCount = len(sample)
	knownTitles := map[string]string{}
	for _, m := range sample {
		knownTitles[recommendationTitleKey(m.Title)] = m.Title
	}
	suggestions, err := recommendations.Generate(ctx, s.Config.GeminiKey, s.Config.GeminiModel, in.Focus, in.Language, in.Preferences, sample)
	if err != nil {
		return result, route.Fail(502, "Gemini could not generate recommendations; check model, credentials and quota")
	}
	for _, candidate := range suggestions {
		if ctx.Err() != nil {
			return result, route.Fail(504, "Recommendation verification timed out")
		}
		related := []string{}
		relatedSeen := map[string]bool{}
		for _, title := range candidate.RelatedTitles {
			if owned, ok := knownTitles[recommendationTitleKey(title)]; ok && !relatedSeen[owned] {
				related = append(related, owned)
				relatedSeen[owned] = true
			}
		}
		if len(related) == 0 || strings.TrimSpace(candidate.Reason) == "" {
			continue
		}
		matches, _, _, err := media.SearchMetadata(ctx, s.Config.TMDBToken, candidate.Title, "movie", candidate.Year, in.Language)
		if err != nil {
			return result, route.Fail(502, "TMDB could not verify movie recommendations")
		}
		if len(matches) == 0 || !matches[0].Recommended {
			continue
		}
		m := matches[0]
		year := 0
		if len(m.Date) >= 4 {
			year, _ = strconv.Atoi(m.Date[:4])
		}
		if released, err := time.Parse("2006-01-02", m.Date); err != nil || released.After(time.Now().UTC()) {
			continue
		}
		if year != candidate.Year || ownedIDs[m.ID] {
			continue
		}
		existing := false
		for _, title := range []string{candidate.Title, m.Title, m.OriginalTitle} {
			key := recommendationTitleKey(title)
			if ownedTitles[key+":"+strconv.Itoa(year)] || ownedTitles[key+":unknown"] {
				existing = true
			}
		}
		if existing {
			continue
		}
		ownedIDs[m.ID] = true
		result.Movies = append(result.Movies, MovieRecommendationDTO{TMDBID: m.ID, Title: m.Title, OriginalTitle: m.OriginalTitle, Year: year, Reason: candidate.Reason, RelatedTitles: related})
	}
	// Recheck the live catalog after the provider calls: imports may have finished.
	current, err := s.DB.RecommendationMovies(ctx)
	if err != nil {
		return result, err
	}
	if len(current) > 10000 {
		return result, route.Fail(422, "Collection exceeds the 10000-title recommendation limit")
	}
	result.Movies = filterOwnedRecommendations(result.Movies, current)
	result.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	err = s.DB.SaveSetting(ctx, store.SaveSettingParams{Key: recommendationKey, Value: data})
	return result, err
}

// Also filter persisted snapshots when the collection has grown since generation.
func filterOwnedRecommendations(movies []MovieRecommendationDTO, rows []store.RecommendationMoviesRow) []MovieRecommendationDTO {
	ids := map[int]bool{}
	titles := map[string]bool{}
	for _, row := range rows {
		id, _ := strconv.Atoi(row.ProviderID)
		if id > 0 {
			ids[id] = true
		}
		key := recommendationTitleKey(row.Title)
		titles[key+":"+strconv.Itoa(int(row.Year))] = true
		if row.Year == 0 {
			titles[key+":unknown"] = true
		}
	}
	filtered := make([]MovieRecommendationDTO, 0, len(movies))
	for _, movie := range movies {
		if ids[movie.TMDBID] {
			continue
		}
		owned := false
		for _, title := range []string{movie.Title, movie.OriginalTitle} {
			key := recommendationTitleKey(title)
			if key != "" && (titles[key+":"+strconv.Itoa(movie.Year)] || titles[key+":unknown"]) {
				owned = true
			}
		}
		if !owned {
			filtered = append(filtered, movie)
		}
	}
	return filtered
}

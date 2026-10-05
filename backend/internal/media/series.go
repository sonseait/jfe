package media

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var seasonDirectory = regexp.MustCompile(`(?i)^(?:s|season|session)?[ ._-]*(\d{1,4})$`)
var shortEpisode = regexp.MustCompile(`(?i)^(?:ep(?:isode)?|e)?[ ._-]*(\d{1,5})(?:$|[ ._-])`)

// ParseSeries uses the containing show directory as identity, not episode titles.
// Flat libraries retain filename grouping for backwards compatibility.
func ParseSeries(root, path string) (Name, string) {
	n := Parse(path)
	root, path = filepath.Clean(root), filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Name{}, ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return n, ""
	}
	// The first folder under the library root owns the show identity.
	dir := filepath.Join(root, parts[0])
	season, inSeason := 1, false
	if len(parts) >= 3 {
		if match := seasonDirectory.FindStringSubmatch(parts[1]); match != nil {
			inSeason = true
			season, _ = strconv.Atoi(match[1])
		} else if strings.EqualFold(parts[1], "Specials") {
			inSeason, season = true, 0
		}
	}
	folder := Parse(filepath.Base(dir) + ".mkv")
	n.Series, n.Year = folder.Title, folder.Year
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if !episodePattern.MatchString(base) {
		n.Season = season
		if match := shortEpisode.FindStringSubmatch(base); match != nil {
			n.Episode, _ = strconv.Atoi(match[1])
		}
	}
	if inSeason {
		n.Season = season
	}
	// Keep unnumbered videos in the show too; do not invent an episode number.
	if n.Episode > 0 {
		n.Title = fmt.Sprintf("%s S%02dE%02d", n.Series, n.Season, n.Episode)
	}
	return n, dir
}

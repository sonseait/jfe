package media

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type CastMember struct {
	Image     string `json:"image,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
}

// Provider identities avoid conflating TMDB actors who share a name. Local
// credits without a provider identity are matched by their normalized name.
func NewCastMember(name, character string, tmdbID int) CastMember {
	name = strings.TrimSpace(name)
	key := "local-person:" + strings.ToLower(strings.Join(strings.Fields(name), " "))
	if tmdbID > 0 {
		key = "tmdb-person:" + strconv.Itoa(tmdbID)
	}
	return CastMember{ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(key)).String(), Name: name, Character: strings.TrimSpace(character)}
}

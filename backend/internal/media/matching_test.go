package media

import "testing"

func TestMetadataQueryFromFilename(t *testing.T) {
	title, year := metadataQuery("Dune.2021.2160p.WEB-DL.x265.mkv", 1984)
	if title != "Dune" || year != 2021 {
		t.Fatalf("%s %d", title, year)
	}
	title, year = metadataQuery("Spider-Man: No Way Home", 2021)
	if title != "Spider-Man: No Way Home" || year != 2021 {
		t.Fatalf("%s %d", title, year)
	}
	title, _ = metadataQuery("Dark.S01E02.1080p.mkv", 0)
	if title != "Dark" {
		t.Fatal(title)
	}
}

func TestRankMatches(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		year        int
		matches     []Match
		wantID      int
	}{
		{"remake", "Dune", 2021, []Match{{ID: 1, Title: "Dune", Date: "1984-01-01"}, {ID: 2, Title: "Dune", Date: "2021-01-01"}}, 2},
		{"ambiguous remake", "Dune", 0, []Match{{ID: 1, Title: "Dune"}, {ID: 2, Title: "Dune"}}, 0},
		{"original title", "Amelie", 2001, []Match{{ID: 1, Title: "Localized", OriginalTitle: "Amélie", Date: "2001-01-01"}}, 1},
		{"sequel", "Alien", 1979, []Match{{ID: 1, Title: "Alien Resurrection", Date: "1997-01-01"}}, 0},
		{"sole result wrong year scores 60", "Dune", 2021, []Match{{ID: 1, Title: "Dune", Date: "1984-01-01"}}, 1},
		{"sole result missing year", "Dune", 2021, []Match{{ID: 1, Title: "Dune"}}, 1},
		{"sole fuzzy result scores 67", "The Quiet Blue Horizon", 2025, []Match{{ID: 1, Title: "The Quiet Horizon", Date: "2025-01-01"}}, 1},
		{"exactly 50 not accepted", "Quiet Horizon", 2025, []Match{{ID: 1, Title: "Horizon", Date: "2025-01-01"}}, 0},
		{"below 50 not accepted", "The Quiet Horizon", 0, []Match{{ID: 1, Title: "Quiet Horizon"}}, 0},
		{"multiple fuzzy results not accepted", "The Quiet Blue Horizon", 2025, []Match{{ID: 1, Title: "The Quiet Horizon", Date: "2025-01-01"}, {ID: 2, Title: "Horizon"}}, 0},
		{"series", "Dark", 2017, []Match{{ID: 1, Name: "Dark", Air: "2017-01-01"}}, 1},
		{"empty", "Dune", 2021, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RankMatches(tc.matches, tc.title, tc.year)
			got := 0
			for _, m := range tc.matches {
				if m.Recommended {
					got = m.ID
				}
			}
			if got != tc.wantID {
				t.Fatalf("recommended %d, want %d: %+v", got, tc.wantID, tc.matches)
			}
		})
	}
}

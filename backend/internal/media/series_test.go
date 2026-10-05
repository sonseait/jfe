package media

import "testing"

func TestParseSeries(t *testing.T) {
	for _, tc := range []struct {
		path, folder, title string
		season, episode     int
	}{
		{"/tv/Show/S01/ep1.mkv", "/tv/Show", "Show", 1, 1},
		{"/tv/Show/02/Wrong.Show.S01E03.mkv", "/tv/Show", "Show", 2, 3},
		{"/tv/Show/Specials/E01.mkv", "/tv/Show", "Show", 0, 1},
		{"/tv/Show/Season 03/Wrong.Show.S01E04.mkv", "/tv/Show", "Show", 3, 4},
		{"/tv/Show/Custom season/ep5.mkv", "/tv/Show", "Show", 1, 5},
		{"/tv/Show/Season 02/Disc 1/ep6.mkv", "/tv/Show", "Show", 2, 6},
		{"/tv/1899/ep1.mkv", "/tv/1899", "1899", 1, 1},
		{"/tv/Show/session 02/ep2.mkv", "/tv/Show", "Show", 2, 2},
		{"/tv/Show/Season 01/Different.Title.S01E03.mkv", "/tv/Show", "Show", 1, 3},
		{"/tv/Show/s00/E01.mkv", "/tv/Show", "Show", 0, 1},
		{"/tv/Show/02 - Intro.mp4", "/tv/Show", "Show", 1, 2},
		{"/tv/Show/Trailer.mp4", "/tv/Show", "Show", 1, 0},
		{"/tv/Other/Season 01/Show.S01E01.mkv", "/tv/Other", "Other", 1, 1},
		{"/tv/Show.S01E01.mkv", "", "Show", 1, 1},
	} {
		t.Run(tc.path, func(t *testing.T) {
			n, folder := ParseSeries("/tv", tc.path)
			if folder != tc.folder || n.Series != tc.title || n.Season != tc.season || n.Episode != tc.episode {
				t.Fatalf("got %+v folder %q", n, folder)
			}
		})
	}
}

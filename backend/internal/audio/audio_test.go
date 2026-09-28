package audio

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestYouTubeURLs(t *testing.T) {
	for _, raw := range []string{"https://youtu.be/abcdefghijk?t=2", "https://music.youtube.com/watch?v=abcdefghijk", "https://www.youtube.com/playlist?list=PLabcdefghijk"} {
		if _, e := YouTubeURL(raw); e != nil {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{"http://youtube.com/watch?v=abcdefghijk", "https://youtube.com.evil.test/watch?v=abcdefghijk", "https://youtube.com:443/watch?v=abcdefghijk", "https://user@youtube.com/watch?v=abcdefghijk", "file:///tmp/test", "https://youtube.com/shorts/", "https://youtube.com/live/", "https://youtube.com/watch?v=--exec"} {
		if _, e := YouTubeURL(raw); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "youtube.com.evil.test"} {
		if allowedYouTubeHost(host) {
			t.Fatal("allowed", host)
		}
	}
}
func TestTagsRoundTrip(t *testing.T) {
	python := os.Getenv("JFE_PYTHON")
	if python == "" {
		python = "python3"
	}
	if e := exec.Command(python, "-c", "import mutagen").Run(); e != nil {
		t.Skip("install requirements-audio.txt and set JFE_PYTHON")
	}
	for _, format := range []struct{ ext, codec string }{{"mp3", "libmp3lame"}, {"flac", "flac"}, {"m4a", "aac"}, {"m4b", "aac"}, {"ogg", "vorbis"}, {"opus", "libopus"}, {"wav", "pcm_s16le"}, {"aiff", "pcm_s16be"}} {
		t.Run(format.ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture."+format.ext)
			args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.3", "-ac", "2", "-strict", "-2", "-c:a", format.codec, path}
			if b, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
				t.Fatalf("fixture %v: %s", e, b)
			}
			title, album, comment := "Nhạc thử nghiệm", "Original album", "Keep this"
			artists := []string{"Artist One", "Artist Two"}
			ctx := context.Background()
			if _, e := Helper(ctx, python, "write", path, &Patch{Title: &title, Album: &album, Artists: &artists, Comment: &comment}); e != nil {
				t.Fatal(e)
			}
			before, e := Fingerprint(path)
			if e != nil {
				t.Fatal(e)
			}
			r, e := Helper(ctx, python, "read", path, nil)
			if e != nil {
				t.Fatal(e)
			}
			if r.Tags.Title != title || !reflect.DeepEqual(r.Tags.Artists, artists) {
				t.Fatalf("round trip: %+v", r)
			}
			same, _ := Fingerprint(path)
			if before != same {
				t.Fatal("reading changed fingerprint")
			}
			title = "Changed"
			if _, e = Helper(ctx, python, "write", path, &Patch{Title: &title}); e != nil {
				t.Fatal(e)
			}
			r, e = Helper(ctx, python, "read", path, nil)
			if e != nil {
				t.Fatal(e)
			}
			if r.Tags.Album != album || r.Tags.Comment != comment || r.Tags.Title != title {
				t.Fatalf("lost unedited tags: %+v", r.Tags)
			}
			empty := ""
			if _, e = Helper(ctx, python, "write", path, &Patch{Title: &empty}); e != nil {
				t.Fatal(e)
			}
			r, e = Helper(ctx, python, "read", path, nil)
			if e != nil || r.Tags.Title != "" {
				t.Fatalf("clear: %+v %v", r, e)
			}
		})
	}
}
func TestWritableRejectsHardlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	if e := os.WriteFile(path, []byte("x"), 0640); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(path, path+".copy"); e != nil {
		t.Fatal(e)
	}
	if Writable(path) == nil {
		t.Fatal("hardlink was writable")
	}
}

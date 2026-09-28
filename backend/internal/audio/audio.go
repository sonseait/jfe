package audio

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"jfe/backend/internal/media"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed tags.py
var tagHelper string

type Tags struct {
	Title         string   `json:"title"`
	Artists       []string `json:"artists"`
	AlbumArtists  []string `json:"albumArtists"`
	Album         string   `json:"album"`
	Track         string   `json:"track"`
	Disc          string   `json:"disc"`
	Date          string   `json:"date"`
	Genres        []string `json:"genres"`
	Composer      string   `json:"composer"`
	Comment       string   `json:"comment"`
	Author        string   `json:"author"`
	Narrator      string   `json:"narrator"`
	MusicBrainzID string   `json:"musicBrainzId"`
}
type Patch struct {
	Title         *string   `json:"title,omitempty" jsonschema:"maxLength=500"`
	Artists       *[]string `json:"artists,omitempty"`
	AlbumArtists  *[]string `json:"albumArtists,omitempty"`
	Album         *string   `json:"album,omitempty" jsonschema:"maxLength=500"`
	Track         *string   `json:"track,omitempty" jsonschema:"maxLength=30"`
	Disc          *string   `json:"disc,omitempty" jsonschema:"maxLength=30"`
	Date          *string   `json:"date,omitempty" jsonschema:"maxLength=30"`
	Genres        *[]string `json:"genres,omitempty"`
	Composer      *string   `json:"composer,omitempty" jsonschema:"maxLength=500"`
	Comment       *string   `json:"comment,omitempty" jsonschema:"maxLength=10000"`
	Author        *string   `json:"author,omitempty" jsonschema:"maxLength=500"`
	Narrator      *string   `json:"narrator,omitempty" jsonschema:"maxLength=500"`
	MusicBrainzID *string   `json:"musicBrainzId,omitempty" jsonschema:"maxLength=100"`
	Artwork       *string   `json:"artwork,omitempty" jsonschema:"maxLength=800000"`
}
type ReadResult struct {
	Tags    Tags   `json:"tags"`
	Artwork string `json:"artwork"`
}

func IsLibrary(kind string) bool {
	return kind == "music" || kind == "podcasts" || kind == "audiobooks"
}
func IsItem(kind string) bool {
	return kind == "track" || kind == "podcast_episode" || kind == "book_part"
}
func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".flac", ".m4a", ".m4b", ".ogg", ".opus", ".wav", ".aiff", ".aif":
		return true
	}
	return false
}
func IsAudio(path string) bool {
	return Supported(path) || strings.EqualFold(filepath.Ext(path), ".aac")
}
func Within(mediaRoot, importRoot, path string) (string, error) {
	if p, e := media.Within(mediaRoot, path); e == nil {
		return p, nil
	}
	if importRoot != "" {
		return media.Within(importRoot, path)
	}
	return "", fmt.Errorf("path outside configured roots")
}
func Fingerprint(path string) (string, error) {
	return FingerprintAt(path, path)
}
func FingerprintAt(path, identityPath string) (string, error) {
	st, e := os.Stat(path)
	if e != nil {
		return "", e
	}
	identity := ""
	if stat, ok := st.Sys().(*syscall.Stat_t); ok {
		identity = fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", identityPath, st.Size(), st.ModTime().UnixNano(), identity)))
	return hex.EncodeToString(h[:]), nil
}
func Writable(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return fmt.Errorf("hardlinked files cannot be edited")
	}
	if !Supported(path) {
		return fmt.Errorf("container does not support tag editing")
	}
	if st.Mode().Perm()&0222 == 0 {
		return fmt.Errorf("file is read only")
	}
	f, e := os.OpenFile(path, os.O_WRONLY, 0)
	if e != nil {
		return e
	}
	return f.Close()
}
func Helper(ctx context.Context, python, operation, path string, patch *Patch) (ReadResult, error) {
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, _ := json.Marshal(patch)
	cmd := exec.CommandContext(ctx, python, "-c", tagHelper, operation, path)
	cmd.Stdin = strings.NewReader(string(data))
	var output limitedOutput
	cmd.Stdout = &output
	e := cmd.Run()
	b := output.data
	if e != nil {
		return ReadResult{}, fmt.Errorf("audio tag helper failed")
	}
	var r ReadResult
	e = json.Unmarshal(b, &r)
	return r, e
}

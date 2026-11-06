package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"jfe/backend/internal/media"
)

type mkvTrack struct {
	ID         int            `json:"id"`
	Type       string         `json:"type"`
	Codec      string         `json:"codec"`
	Properties map[string]any `json:"properties"`
}
type mkvIdentity struct {
	Tracks      []mkvTrack       `json:"tracks"`
	Attachments []map[string]any `json:"attachments"`
	Chapters    []map[string]any `json:"chapters"`
	GlobalTags  []map[string]any `json:"global_tags"`
	TrackTags   []map[string]any `json:"track_tags"`
}

func identifyMKV(ctx context.Context, path string) (mkvIdentity, error) {
	var v mkvIdentity
	b, e := exec.CommandContext(ctx, "mkvmerge", "-J", path).Output()
	if e != nil {
		return v, e
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	e = decoder.Decode(&v)
	return v, e
}
func (w *Worker) remuxSubtitle(ctx context.Context, source, tmp string, track media.Stream, action string, offset int) error {
	before, e := identifyMKV(ctx, source)
	if e != nil {
		return e
	}
	probe, e := media.Inspect(ctx, source)
	if e != nil {
		return e
	}
	ordinal := -1
	seen := 0
	for _, s := range probe.Streams {
		if s.Type != "subtitle" {
			continue
		}
		if s.Index == track.Index {
			ordinal = seen
			if s.Codec != track.Codec {
				return subtitleError("subtitle_changed")
			}
			break
		}
		seen++
	}
	if ordinal < 0 {
		return subtitleError("subtitle_changed")
	}
	subs := []mkvTrack{}
	for _, t := range before.Tracks {
		if t.Type == "subtitles" {
			subs = append(subs, t)
		}
	}
	if ordinal >= len(subs) {
		return subtitleError("subtitle_changed")
	}
	target := subs[ordinal]
	if action == "save" {
		if !media.SubtitleTextCodec(track.Codec) {
			return media.ErrSubtitleFormat
		}
		cues, e := w.subtitleCues(ctx, source, track)
		if e != nil {
			return e
		}
		for _, c := range cues {
			if c.Start+float64(offset)/1000 < 0 {
				return media.ErrSubtitleNegative
			}
		}
	}
	_ = os.Remove(tmp)
	args := []string{"--engage", "keep_track_statistics_tags", "--disable-track-statistics-tags", "--normalize-language-ietf", "off", "-o", tmp}
	if action == "delete" {
		if len(subs) == 1 {
			args = append(args, "--no-subtitles")
		} else {
			args = append(args, "--subtitle-tracks", "!"+strconv.Itoa(target.ID))
		}
	} else {
		args = append(args, "--sync", fmt.Sprintf("%d:%d", target.ID, offset))
	}
	if len(before.Chapters) > 0 {
		chapterXML, e := exec.CommandContext(ctx, "mkvextract", source, "chapters").Output()
		if e != nil {
			return e
		}
		chapterPath := tmp + ".chapters.xml"
		defer os.Remove(chapterPath)
		if e = os.WriteFile(chapterPath, chapterXML, 0600); e != nil {
			return e
		}
		args = append(args, "--chapters", chapterPath, "--no-chapters")
	}
	args = append(args, source)
	cmd := exec.CommandContext(ctx, "mkvmerge", args...)
	e = cmd.Run()
	// mkvmerge exit 1 denotes warnings; output still needs full verification.
	if e != nil {
		if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return e
		}
	}
	after, e := identifyMKV(ctx, tmp)
	if e != nil {
		return e
	}
	retained := []mkvTrack{}
	for _, t := range before.Tracks {
		if action == "delete" && t.ID == target.ID {
			continue
		}
		retained = append(retained, t)
	}
	if len(retained) != len(after.Tracks) {
		return fmt.Errorf("subtitle_verification_failed: output track count")
	}
	keys := []string{"uid", "codec_id", "language", "language_ietf", "track_name", "default_track", "forced_track", "enabled_track", "hearing_impaired", "visual_impaired", "text_descriptions", "original", "commentary"}
	for n, t := range retained {
		a := after.Tracks[n]
		if t.Type != a.Type || t.Codec != a.Codec {
			return fmt.Errorf("subtitle_verification_failed: track type/codec")
		}
		for _, key := range keys {
			// Older Matroska files omit LanguageIETF; mkvmerge adds its derived default.
			if key == "language_ietf" && t.Properties[key] == nil {
				continue
			}
			if !reflect.DeepEqual(t.Properties[key], a.Properties[key]) {
				return fmt.Errorf("subtitle_verification_failed: property %s: %v -> %v", key, t.Properties[key], a.Properties[key])
			}
		}
	}
	if !reflect.DeepEqual(before.Attachments, after.Attachments) || !reflect.DeepEqual(before.Chapters, after.Chapters) || !reflect.DeepEqual(before.GlobalTags, after.GlobalTags) {
		return fmt.Errorf("subtitle_verification_failed: attachments/chapters/global tags")
	}
	removedUID := ""
	if action == "delete" {
		removedUID = fmt.Sprint(target.Properties["uid"])
	}
	if e = verifyMKVMetadata(ctx, source, tmp, removedUID); e != nil {
		return e
	}
	// Verify packet payloads (including subtitles) without decoding or encoding.
	// Stream numbers may change after deleting a subtitle; compare retained order.
	ap, e := media.Inspect(ctx, tmp)
	if e != nil {
		return e
	}
	kept := []media.Stream{}
	for _, s := range probe.Streams {
		if action == "delete" && s.Index == track.Index {
			continue
		}
		kept = append(kept, s)
	}
	if len(kept) != len(ap.Streams) {
		return fmt.Errorf("subtitle_verification_failed: stream count")
	}
	a, e := packetDigests(ctx, source)
	if e != nil {
		return e
	}
	b, e := packetDigests(ctx, tmp)
	if e != nil {
		return e
	}
	for n, s := range kept {
		original, updated := a[s.Index], b[ap.Streams[n].Index]
		if original.Digest != updated.Digest {
			return fmt.Errorf("subtitle_verification_failed: packet payload of stream %d", s.Index)
		}
		if s.Index != track.Index {
			if len(original.Times) != len(updated.Times) {
				return fmt.Errorf("subtitle_verification_failed: packet timing count of stream %d", s.Index)
			}
			for i, pts := range original.Times {
				shifted := updated.Times[i]
				if (pts == math.MinInt64) != (shifted == math.MinInt64) || abs(float64(pts)-float64(shifted)) > 2 {
					return fmt.Errorf("subtitle_verification_failed: packet timing of stream %d", s.Index)
				}
			}
		}
	}
	if action == "save" {
		original, e := w.subtitleCues(ctx, source, track)
		if e != nil {
			return e
		}
		var updated media.Stream
		for _, s := range ap.Streams {
			if s.Type == "subtitle" {
				if ordinal == 0 {
					updated = s
					break
				}
				ordinal--
			}
		}
		shifted, e := w.subtitleCues(ctx, tmp, updated)
		if e != nil {
			return e
		}
		if len(original) != len(shifted) {
			return fmt.Errorf("subtitle_verification_failed: cue count")
		}
		for n, c := range original {
			d := shifted[n]
			delta := float64(offset) / 1000
			if c.Text != d.Text || abs(c.Start+delta-d.Start) > 0.011 || abs(c.End+delta-d.End) > 0.011 {
				return fmt.Errorf("subtitle_verification_failed: cue %d: %.3f/%.3f -> %.3f/%.3f, offset %.3f", n, c.Start, c.End, d.Start, d.End, delta)
			}
		}
	}
	return nil
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

type packetSignature struct {
	Digest string
	Times  []int64
}

func packetDigests(ctx context.Context, path string) (map[int]packetSignature, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_packets", "-show_streams", "-show_data_hash", "sha256", "-show_entries", "packet=stream_index,size,data_hash,pts_time:stream=index,extradata_hash", "-of", "json", path)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	d := json.NewDecoder(pipe)
	hashes := map[int]hash.Hash{}
	times := map[int][]int64{}
	consume := func() error {
		if _, e := d.Token(); e != nil {
			return e
		}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			if key == "streams" {
				var streams []struct {
					Index int    `json:"index"`
					Extra string `json:"extradata_hash"`
				}
				if e = d.Decode(&streams); e != nil {
					return e
				}
				for _, stream := range streams {
					h := hashes[stream.Index]
					if h == nil {
						h = sha256.New()
						hashes[stream.Index] = h
					}
					_, _ = io.WriteString(h, "extra:"+stream.Extra+"\n")
				}
				continue
			}
			if key != "packets" {
				var discard json.RawMessage
				if e = d.Decode(&discard); e != nil {
					return e
				}
				continue
			}
			if _, e = d.Token(); e != nil {
				return e
			}
			for d.More() {
				var p struct {
					Index int    `json:"stream_index"`
					Size  string `json:"size"`
					Hash  string `json:"data_hash"`
					PTS   string `json:"pts_time"`
				}
				if e = d.Decode(&p); e != nil {
					return e
				}
				h := hashes[p.Index]
				if h == nil {
					h = sha256.New()
					hashes[p.Index] = h
				}
				_, _ = io.WriteString(h, p.Size+":"+p.Hash+"\n")
				pts := int64(math.MinInt64)
				if value, e := strconv.ParseFloat(p.PTS, 64); e == nil {
					pts = int64(math.Round(value * 1000))
				}
				times[p.Index] = append(times[p.Index], pts)
			}
			if _, e = d.Token(); e != nil {
				return e
			}
		}
		_, e := d.Token()
		return e
	}
	if e = consume(); e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, e
	}
	if e = cmd.Wait(); e != nil {
		return nil, e
	}
	result := map[int]packetSignature{}
	for i, h := range hashes {
		result[i] = packetSignature{Digest: hex.EncodeToString(h.Sum(nil)), Times: times[i]}
	}
	return result, nil
}

// mkvextract emits stable XML for metadata. Only tags targeting the removed
// track may disappear; all other tag bodies and chapter bodies must survive.
func verifyMKVMetadata(ctx context.Context, before, after string, removedUID string) error {
	for _, kind := range []string{"chapters", "tags"} {
		a, e := exec.CommandContext(ctx, "mkvextract", before, kind).Output()
		if e != nil {
			return e
		}
		b, e := exec.CommandContext(ctx, "mkvextract", after, kind).Output()
		if e != nil {
			return e
		}
		if kind == "chapters" {
			if !sameChapterXML(a, b) {
				return fmt.Errorf("subtitle_verification_failed: chapter contents")
			}
			continue
		}
		type tag struct {
			Targets struct {
				TrackUID []string `xml:"TrackUID"`
			} `xml:"Targets"`
			Body string `xml:",innerxml"`
		}
		var old, new struct {
			Tags []tag `xml:"Tag"`
		}
		if len(bytes.TrimSpace(a)) > 0 {
			if e = xml.Unmarshal(a, &old); e != nil {
				return e
			}
		}
		if len(bytes.TrimSpace(b)) > 0 {
			if e = xml.Unmarshal(b, &new); e != nil {
				return e
			}
		}
		kept := []string{}
		for _, t := range old.Tags {
			if removedUID != "" && len(t.Targets.TrackUID) == 1 && t.Targets.TrackUID[0] == removedUID {
				continue
			}
			kept = append(kept, strings.TrimSpace(t.Body))
		}
		actual := []string{}
		for _, t := range new.Tags {
			actual = append(actual, strings.TrimSpace(t.Body))
		}
		slices.Sort(kept)
		slices.Sort(actual)
		if !reflect.DeepEqual(kept, actual) {
			return fmt.Errorf("subtitle_verification_failed: tag contents")
		}
	}
	return nil
}

type chapterXMLNode struct {
	Name     xml.Name         `xml:""`
	Text     string           `xml:",chardata"`
	Children []chapterXMLNode `xml:",any"`
}

func (n *chapterXMLNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type plain chapterXMLNode
	var v plain
	if e := d.DecodeElement(&v, &start); e != nil {
		return e
	}
	*n = chapterXMLNode(v)
	n.Name = start.Name
	n.Text = strings.TrimSpace(n.Text)
	return nil
}
func sameChapterXML(a, b []byte) bool {
	var old, new chapterXMLNode
	if len(bytes.TrimSpace(a)) == 0 && len(bytes.TrimSpace(b)) == 0 {
		return true
	}
	if xml.Unmarshal(a, &old) != nil || xml.Unmarshal(b, &new) != nil {
		return false
	}
	var compare func(chapterXMLNode, chapterXMLNode) bool
	compare = func(x, y chapterXMLNode) bool {
		if x.Name != y.Name || x.Text != y.Text {
			return false
		}
		has := map[string]bool{}
		for _, c := range x.Children {
			has[c.Name.Local] = true
		}
		kept := []chapterXMLNode{}
		for _, c := range y.Children {
			// mkvmerge supplies missing edition identities and default IETF language.
			// Existing identities/languages, chapter boundaries and titles must match.
			if !has[c.Name.Local] && (c.Name.Local == "EditionUID" || c.Name.Local == "ChapLanguageIETF") {
				continue
			}
			kept = append(kept, c)
		}
		if len(x.Children) != len(kept) {
			return false
		}
		for n, c := range x.Children {
			if !compare(c, kept[n]) {
				return false
			}
		}
		return true
	}
	return compare(old, new)
}

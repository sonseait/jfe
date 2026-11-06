package worker

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
)

func remuxOutputArgs() []string {
	return []string{"-sn", "-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "4000000", "-flush_packets", "1", "-f", "mp4", "stream.mp4"}
}

// Only publish readiness after a complete media fragment. Inspect box headers
// with constant memory, even when the first source GOP is very large.
func remuxHasInitialBuffer(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "stream.mp4"))
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	var header [16]byte
	var offset int64
	moof := false
	for offset+8 <= stat.Size() {
		if _, err = f.ReadAt(header[:8], offset); err != nil {
			return false
		}
		size := int64(binary.BigEndian.Uint32(header[:4]))
		headerSize := int64(8)
		if size == 1 {
			if _, err = f.ReadAt(header[8:], offset+8); err != nil {
				return false
			}
			size = int64(binary.BigEndian.Uint64(header[8:]))
			headerSize = 16
		}
		if size < headerSize || size > stat.Size()-offset {
			return false
		}
		box := string(header[4:8])
		if box == "moof" {
			moof = true
		}
		if box == "mdat" && moof {
			return true
		}
		offset += size
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return false
		}
	}
	return false
}

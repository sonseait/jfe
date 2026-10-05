package server

import (
	"fmt"
	"github.com/gofiber/fiber/v3"
	"io"
	"os"
	"strconv"
	"strings"
)

type remuxRangeReader struct {
	*io.SectionReader
	file *os.File
}

func (r *remuxRangeReader) Close() error { return r.file.Close() }

// SendFile caches file size, which is unsafe for a growing worker output.
// Each request gets a fresh size snapshot and streams a bounded byte range.
func sendRemuxRange(c fiber.Ctx, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	c.Set("Accept-Ranges", "bytes")
	parts := strings.Split(strings.TrimPrefix(c.Get("Range"), "bytes="), "-")
	start, end := int64(-1), int64(-1)
	if strings.HasPrefix(c.Get("Range"), "bytes=") && len(parts) == 2 {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err == nil {
			end = stat.Size() - 1
			if parts[1] != "" {
				end, err = strconv.ParseInt(parts[1], 10, 64)
			}
		}
	}
	if err != nil || start < 0 || start >= stat.Size() || end < start {
		f.Close()
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", stat.Size()))
		return c.SendStatus(416)
	}
	end = min(end, stat.Size()-1, start+1024*1024-1)
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()))
	c.Status(206)
	return c.SendStream(&remuxRangeReader{SectionReader: io.NewSectionReader(f, start, end-start+1), file: f}, int(end-start+1))
}

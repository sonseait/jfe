package server

import (
	"github.com/gofiber/fiber/v3"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGrowingRemuxRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.mp4")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/stream", func(c fiber.Ctx) error { return sendRemuxRange(c, path) })
	request := func(r string, status int, contentRange, body string) {
		t.Helper()
		req := httptest.NewRequest("GET", "/stream", nil)
		req.Header.Set("Range", r)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != status || res.Header.Get("Content-Range") != contentRange || (status == 206 && string(data) != body) || res.Header.Get("Accept-Ranges") != "bytes" {
			t.Fatalf("%d %v %q", res.StatusCode, res.Header, string(data))
		}
		if status == 206 && res.ContentLength != int64(len(body)) {
			t.Fatalf("length %d", res.ContentLength)
		}
	}
	request("bytes=0-3", 206, "bytes 0-3/5", "firs")
	request("bytes=5-9", 416, "bytes */5", "")
	if err := os.WriteFile(path, []byte("first-more"), 0600); err != nil {
		t.Fatal(err)
	}
	request("bytes=5-999", 206, "bytes 5-9/10", "-more")
	request("bytes=0-1,3-4", 416, "bytes */10", "")
}

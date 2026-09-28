package worker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPortraitMissingAndUnsafe(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	w := Worker{}
	for _, path := range []string{"", "https://other.test/a.jpg", "/../a.jpg", "//other.test/a.jpg", "/actor.jpg"} {
		got, err := w.castPortrait(context.Background(), "actor", path)
		if err != nil || got != "" {
			t.Fatalf("%q: %q %v", path, got, err)
		}
	}
	if requests != 1 {
		t.Fatalf("unsafe paths made requests: %d", requests)
	}
	if err := w.savePortrait("actor", strings.NewReader("not an image")); err == nil {
		t.Fatal("non-image accepted")
	}
}

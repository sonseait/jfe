package settings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type reader struct {
	data string
	err  error
}

func (r reader) GetSetting(context.Context, string) ([]byte, error) { return []byte(r.data), r.err }
func TestDefaultsAndPartialValues(t *testing.T) {
	got, err := Load(context.Background(), reader{err: pgx.ErrNoRows})
	if err != nil || got != Defaults() {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	got, err = Load(context.Background(), reader{data: `{"autoMetadata":false,"castImages":false,"metadataLanguage":"vi-VN","future":42}`})
	if err != nil || got.AutoMetadata || got.CastImages || got.WatchedPercent != 95 || got.MetadataLanguage != "vi-VN" {
		t.Fatalf("partial: %+v %v", got, err)
	}
	for _, data := range []string{`{"watchedPercent":0}`, `{"serverName":" "}`, `{"metadataLanguage":"invalid"}`, `broken`} {
		if _, err := Load(context.Background(), reader{data: data}); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	if _, err := Load(context.Background(), reader{err: errors.New("offline")}); err == nil {
		t.Fatal("database failure hidden")
	}
}

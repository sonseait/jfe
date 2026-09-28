package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
)

type systemDB struct {
	store.DBTX
	countErr, settingsErr error
	settings              []byte
}

type scanRow func(...any) error

func (r scanRow) Scan(dest ...any) error { return r(dest...) }

func (db systemDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return scanRow(func(dest ...any) error {
		if strings.Contains(sql, "CountUsers") {
			*dest[0].(*int64) = 0
			return db.countErr
		}
		*dest[0].(*[]byte) = db.settings
		return db.settingsErr
	})
}

func TestSystemErrorLogging(t *testing.T) {
	for _, tc := range []struct {
		name, cause string
		db          systemDB
	}{
		{"count", "get system: count users: database offline", systemDB{countErr: errors.New("database offline")}},
		{"missing setting", "get system: read encoding settings: no rows", systemDB{settingsErr: pgx.ErrNoRows}},
		{"invalid setting", "get system: parse encoding settings: unsupported video encoder mode", systemDB{settings: []byte(`{"mode":"cpu"}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := log.Logger
			log.Logger = zerolog.New(&logs)
			defer func() { log.Logger = previous }()
			s := New(config.Config{}, nil)
			s.DB = store.New(tc.db)
			req := httptest.NewRequest("GET", "/api/v1/system?token=private-query", nil)
			req.Header.Set("Authorization", "Bearer private-header")
			req.Header.Set("X-Request-ID", "system-test")
			res, err := s.App.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != 500 || !bytes.Contains(body, []byte("The request could not be completed")) || bytes.Contains(body, []byte(tc.cause)) {
				t.Fatalf("unexpected public error: %d %s", res.StatusCode, body)
			}
			if strings.Contains(logs.String(), "private-") {
				t.Fatalf("credentials logged: %s", logs.String())
			}
			decoder := json.NewDecoder(&logs)
			for _, level := range []string{"error", "info"} {
				var event map[string]any
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event["level"] != level || event["status"] != float64(500) || event["requestId"] != "system-test" || event["route"] != "/api/v1/system" {
					t.Fatalf("unexpected log: %+v", event)
				}
				if level == "error" && !strings.Contains(event["error"].(string), tc.cause) {
					t.Fatalf("missing cause: %+v", event)
				}
			}
		})
	}
}

func TestUnhandledErrorsAreLoggedWithFinalStatus(t *testing.T) {
	for _, panicHandler := range []bool{false, true} {
		var logs bytes.Buffer
		previous := log.Logger
		log.Logger = zerolog.New(&logs)
		s := New(config.Config{}, nil)
		s.App.Get("/failure", func(c fiber.Ctx) error {
			if panicHandler {
				panic("handler panic")
			}
			return errors.New("stream failed")
		})
		res, err := s.App.Test(httptest.NewRequest("GET", "/failure", nil))
		log.Logger = previous
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 500 || strings.Count(logs.String(), `"level":"error"`) != 1 || strings.Count(logs.String(), `"status":500`) != 2 {
			t.Fatalf("missing or duplicate error/access logs: %s", logs.String())
		}
	}
}

package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type Server struct {
	Config config.Config
	Pool   *pgxpool.Pool
	DB     *store.Queries
	App    *fiber.App
	Routes *route.Registry
}

func New(cfg config.Config, pool *pgxpool.Pool) *Server {
	app := fiber.New(fiber.Config{Immutable: true, BodyLimit: 1 << 20, ErrorHandler: route.ErrorHandler})
	app.Use(requestid.New())
	app.Use(func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		if err != nil {
			err = route.ErrorHandler(c, err)
		}
		log.Info().Str("requestId", c.GetRespHeader("X-Request-ID")).Str("method", c.Method()).Str("route", c.Route().Path).Int("status", c.Response().StatusCode()).Dur("duration", time.Since(start)).Msg("request")
		return err
	})
	app.Use(recover.New())
	app.Use("/api/v1/auth", limiter.New(limiter.Config{Max: 20, Expiration: time.Minute}))
	s := &Server{cfg, pool, nil, app, route.New(app)}
	if pool != nil {
		s.DB = store.New(pool)
	}
	s.Routes.Auth = s.authenticate
	s.register()
	s.Routes.Docs()
	return s
}
func (s *Server) authenticate(ctx context.Context, token string) (route.Principal, error) {
	if s.DB == nil {
		return route.Principal{}, route.Fail(503, "Database unavailable")
	}
	u, err := s.DB.Authenticate(ctx, hash(token))
	if err != nil {
		return route.Principal{}, route.Fail(401, "Authentication required")
	}
	return route.Principal{ID: u.ID, Role: u.Role}, nil
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func token() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func dbError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return route.Fail(404, "Not found")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return route.Fail(409, "Already exists")
	}
	return err
}
func out[T any](v T, err error) (route.Output[T], error) {
	return route.Output[T]{Body: v}, dbError(err)
}
func op(path, id, access string) route.Operation {
	return route.Operation{Path: "/api/v1" + path, ID: id, Summary: id, Access: access, Tag: "jfe"}
}
func (s *Server) access(ctx context.Context, libraryID string) error {
	p := route.User(ctx)
	ok, err := s.DB.CanAccess(ctx, store.CanAccessParams{ID: libraryID, UserID: p.ID, Column3: p.Role == "admin"})
	if err != nil {
		return err
	}
	if !ok {
		return route.Fail(http.StatusForbidden, "Library access denied")
	}
	return nil
}

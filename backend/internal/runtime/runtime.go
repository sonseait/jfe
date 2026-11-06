package runtime

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func Start(service string) (context.Context, context.CancelFunc, config.Config, *pgxpool.Pool, error) {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.DurationFieldUnit = time.Millisecond
	log.Logger = zerolog.New(os.Stderr).Level(zerolog.InfoLevel).With().Timestamp().Str("service", service).Int("pid", os.Getpid()).Logger()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cfg, err := config.Load()
	if err != nil {
		return ctx, cancel, cfg, nil, err
	}
	logger, err := newLogger(os.Stderr, cfg)
	if err != nil {
		return ctx, cancel, cfg, nil, err
	}
	logger = logger.With().Str("service", service).Int("pid", os.Getpid()).Logger()
	log.Logger = logger
	zerolog.DefaultContextLogger = &log.Logger
	ctx = logger.WithContext(ctx)
	log.Info().Str("logLevel", cfg.LogLevel).Str("logFormat", cfg.LogFormat).Msg("backend configuration loaded")
	log.Info().Msg("Connecting to PostgreSQL")
	connectedAt := time.Now()
	pool, e := pgxpool.New(ctx, cfg.DatabaseURL)
	if e == nil {
		pingCtx, end := context.WithTimeout(ctx, 10*time.Second)
		e = pool.Ping(pingCtx)
		end()
	}
	if e == nil {
		log.Info().Dur("durationMs", time.Since(connectedAt)).Int32("maxConnections", pool.Config().MaxConns).Msg("PostgreSQL connected")
	}
	if e != nil {
		e = fmt.Errorf("connect to PostgreSQL: %w", e)
	}
	return ctx, cancel, cfg, pool, e
}

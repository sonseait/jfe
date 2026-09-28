package runtime

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func Start() (context.Context, context.CancelFunc, config.Config, *pgxpool.Pool, error) {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(os.Stderr).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cfg, err := config.Load()
	if err != nil {
		return ctx, cancel, cfg, nil, err
	}
	logger, err := newLogger(os.Stderr, cfg)
	if err != nil {
		return ctx, cancel, cfg, nil, err
	}
	log.Logger = logger
	log.Info().Msg("Backend configuration loaded")
	log.Info().Msg("Connecting to PostgreSQL")
	pool, e := pgxpool.New(ctx, cfg.DatabaseURL)
	if e == nil {
		pingCtx, end := context.WithTimeout(ctx, 10*time.Second)
		e = pool.Ping(pingCtx)
		end()
	}
	if e == nil {
		log.Info().Msg("PostgreSQL connected")
	}
	return ctx, cancel, cfg, pool, e
}

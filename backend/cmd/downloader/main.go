package main

import (
	"github.com/rs/zerolog/log"
	rt "jfe/backend/internal/runtime"
	"jfe/backend/internal/worker"
)

func main() {
	ctx, cancel, cfg, pool, e := rt.Start()
	defer cancel()
	if e != nil {
		log.Fatal().Err(e).Msg("database unavailable")
	}
	defer pool.Close()
	if e = worker.Run(ctx, cfg, pool, "downloader"); e != nil {
		log.Fatal().Err(e).Msg("worker failed")
	}
}

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"

	"jfe/backend/internal/config"
	rt "jfe/backend/internal/runtime"
	"jfe/backend/internal/server"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Error().Err(err).Msg("API exited")
		os.Exit(1)
	}
}

const usage = "usage: api [openapi|help]\nDatabase migrations: make -C backend migrate from the repository root (official migrate CLI)."

func run(args []string, output io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("%s", usage)
	}
	if len(args) == 1 {
		switch args[0] {
		case "help", "-h", "--help":
			_, err := fmt.Fprintln(output, usage)
			return err
		case "openapi":
			s := server.New(config.Config{}, nil)
			data, err := s.Routes.JSON()
			if err != nil {
				return fmt.Errorf("export OpenAPI: %w", err)
			}
			_, err = fmt.Fprintln(output, string(data))
			return err
		default:
			return fmt.Errorf("%s", usage)
		}
	}
	ctx, cancel, cfg, pool, err := rt.Start()
	defer cancel()
	if pool != nil {
		defer pool.Close()
	}
	if err != nil {
		return fmt.Errorf("initialize API: %w", err)
	}
	s := server.New(cfg, pool)
	s.App.Hooks().OnListen(func(data fiber.ListenData) error {
		host := data.Host
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "localhost"
		}
		scheme := "http"
		if data.TLS {
			scheme = "https"
		}
		url := scheme + "://" + net.JoinHostPort(host, data.Port)
		log.Info().Str("address", net.JoinHostPort(data.Host, data.Port)).Str("url", url).Str("docs", url+"/docs").Msg("API listening")
		return nil
	})
	shutdown := make(chan error, 1)
	s.App.Hooks().OnPostShutdown(func(err error) error {
		if err == nil {
			log.Info().Msg("API stopped")
		}
		shutdown <- err
		return nil
	})
	if err = s.App.Listen(cfg.Listen, fiber.ListenConfig{
		GracefulContext:       ctx,
		ShutdownTimeout:       10 * time.Second,
		DisableStartupMessage: true,
	}); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// Keep the database pool open until active requests have finished shutting down.
	if err = <-shutdown; err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

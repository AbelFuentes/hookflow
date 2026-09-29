package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AbelFuentes/hookflow/internal/action"
	"github.com/AbelFuentes/hookflow/internal/alexa"
	"github.com/AbelFuentes/hookflow/internal/config"
	"github.com/AbelFuentes/hookflow/internal/engine"
	"github.com/AbelFuentes/hookflow/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := getenv("HOOKFLOW_ADDR", ":8080")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rules, err := config.Load(getenv("HOOKFLOW_RULES", "rules/rules.yaml"), action.DefaultRegistry(log))
	if err != nil {
		return err
	}
	log.Info("rules loaded", "count", len(rules))

	eng := engine.New(log, rules...)

	var routes []server.Route
	if skillID := os.Getenv("HOOKFLOW_ALEXA_SKILL_ID"); skillID != "" {
		routes = append(routes, server.Route{
			Pattern: "POST /alexa",
			Handler: alexa.NewHandler(skillID, alexa.NewVerifier(), eng, log),
		})
		log.Info("alexa endpoint enabled", "path", "/alexa")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.Logging(log, server.New(eng, log, routes...)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

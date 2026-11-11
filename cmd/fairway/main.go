// Fairway — Stellar payment-corridor health monitor.
// See README.md and docs/corridor-verification.md for context.
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

	"github.com/gofairway/fairway/internal/api"
	"github.com/gofairway/fairway/internal/config"
	"github.com/gofairway/fairway/internal/measure"
	"github.com/gofairway/fairway/internal/reference"
	"github.com/gofairway/fairway/internal/scheduler"
	"github.com/gofairway/fairway/internal/seed"
	"github.com/gofairway/fairway/internal/store"
	"github.com/gofairway/fairway/internal/webhook"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// --- Database ---
	st, err := store.New(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := st.Ping(pingCtx); err != nil {
		return err
	}
	logger.Info("database connected")

	// --- Seed corridors from YAML ---
	n, err := seed.Corridors(ctx, st, cfg.Corridors, logger)
	if err != nil {
		return err
	}
	logger.Info("corridors seeded", "count", n)

	// --- Components ---
	refFetcher := reference.NewFrankfurterFetcher(cfg.ReferenceRateURL)
	horizonClient := measure.NewHorizonClient(cfg.HorizonURL, refFetcher)
	dispatcher := webhook.NewDispatcher(cfg.WebhookURL, webhook.Format(cfg.WebhookFormat), logger)
	sched := scheduler.New(
		st, horizonClient, dispatcher, logger,
		cfg.SellAmount, cfg.MeasureInterval,
		cfg.DegradedLossPct, cfg.UnusableLossPct,
	)

	// --- HTTP server ---
	apiServer := api.New(st, logger, api.Config{
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		RateLimitEnabled:   cfg.RateLimitEnabled,
		RateLimitRPS:       cfg.RateLimitRPS,
		RateLimitBurst:     cfg.RateLimitBurst,
		TrustProxy:         cfg.TrustProxy,
	})
	defer apiServer.Close()
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           apiServer,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	// Run HTTP server in background.
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
		}
	}()

	// Run scheduler — blocks until context cancelled.
	go sched.Run(ctx)

	// Wait for shutdown signal.
	<-ctx.Done()
	logger.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	return httpServer.Shutdown(shutCtx)
}

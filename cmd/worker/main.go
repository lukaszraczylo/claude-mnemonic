// Package main provides the entry point for the worker service.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/worker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var Version = "dev"

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().
		Str("version", Version).
		Msg("Starting claude-mnemonic worker")

	// Create service with version
	svc, err := worker.NewService(Version)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create service")
	}

	// Start service
	if err := svc.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start service")
	}

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Received shutdown signal")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := svc.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Shutdown error")
	}

	log.Info().Msg("Worker shutdown complete")
}

// Package main provides the MCP server entry point for claude-mnemonic.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/mcp"
	"github.com/lukaszraczylo/claude-mnemonic/internal/telemetry"
	"github.com/lukaszraczylo/claude-mnemonic/internal/watcher"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	// Parse flags
	project := flag.String("project", "", "Project name (required)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Setup logging - MCP uses stdout for communication, so log to stderr
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true})

	if *project == "" {
		log.Fatal().Msg("--project is required")
	}

	// Get worker port from config
	port := config.GetWorkerPort()
	workerURL := fmt.Sprintf("http://localhost:%d", port)

	// Create HTTP client for worker
	client := &http.Client{Timeout: 30 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info().Msg("Shutting down MCP server")
		cancel()
	}()

	// Start file watchers for config changes
	startWatchers()

	telemetry.Ping("claude-mnemonic", Version)

	// Create and run MCP server
	server := mcp.NewServer(client, workerURL, *project, Version)
	log.Info().Str("project", *project).Str("version", Version).Str("worker", workerURL).Msg("Starting MCP server")

	if err := server.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("MCP server error")
	}
}

// startWatchers initializes file watchers for config.
func startWatchers() {
	// Watch config file for changes (triggers process exit for restart)
	configPath := config.SettingsPath()
	configWatcher, err := watcher.New(configPath, func() {
		log.Warn().Str("path", configPath).Msg("Config file changed, exiting for restart...")
		time.Sleep(100 * time.Millisecond) // Give logs time to flush
		os.Exit(0)
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create config watcher")
	} else {
		if err := configWatcher.Start(); err != nil {
			log.Warn().Err(err).Msg("Failed to start config watcher")
		} else {
			log.Info().Str("path", configPath).Msg("Config file watcher started")
		}
	}
}

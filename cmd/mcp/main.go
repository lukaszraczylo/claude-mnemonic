// Package main provides the MCP server entry point for claude-mnemonic.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/internal/mcp"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/chroma"
	"github.com/lukaszraczylo/claude-mnemonic/internal/watcher"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	// Parse flags
	project := flag.String("project", "", "Project name (required)")
	dataDir := flag.String("data-dir", "", "Data directory (default: ~/.claude-mnemonic)")
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

	// Ensure data directory, vector-db, and settings exist
	if err := config.EnsureAll(); err != nil {
		log.Fatal().Err(err).Msg("Failed to ensure data directories")
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load config, using defaults")
		cfg = config.Default()
	}

	// Override data directory if specified
	dbPath := cfg.DBPath
	vectorDBPath := cfg.VectorDBPath
	if *dataDir != "" {
		dbPath = *dataDir + "/claude-mnemonic.db"
		vectorDBPath = *dataDir + "/vector-db"
	}

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

	// Initialize SQLite store (migrations run automatically)
	storeCfg := sqlite.StoreConfig{
		Path:     dbPath,
		MaxConns: cfg.MaxConns,
		WALMode:  true,
	}
	store, err := sqlite.NewStore(storeCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize SQLite store")
	}
	defer store.Close()

	// Initialize stores
	observationStore := sqlite.NewObservationStore(store)
	summaryStore := sqlite.NewSummaryStore(store)
	promptStore := sqlite.NewPromptStore(store)

	// Initialize ChromaDB client (optional)
	var chromaClient *chroma.Client
	chromaCfg := chroma.Config{
		Project:   *project,
		DataDir:   vectorDBPath,
		PythonVer: cfg.PythonVersion,
		BatchSize: 100,
	}
	chromaClient, err = chroma.NewClient(chromaCfg)
	if err != nil {
		log.Warn().Err(err).Msg("ChromaDB unavailable, vector search disabled")
	} else {
		if err := chromaClient.Connect(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to connect to ChromaDB, vector search disabled")
			chromaClient = nil
		} else {
			defer chromaClient.Close()
		}
	}

	// Initialize search manager
	searchMgr := search.NewManager(observationStore, summaryStore, promptStore, chromaClient)

	// Start file watchers
	startWatchers(ctx, vectorDBPath, chromaClient)

	// Create and run MCP server
	server := mcp.NewServer(searchMgr, Version)
	log.Info().Str("project", *project).Str("version", Version).Msg("Starting MCP server")

	if err := server.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("MCP server error")
	}
}

// startWatchers initializes file watchers for vector DB and config.
func startWatchers(ctx context.Context, vectorDBPath string, chromaClient *chroma.Client) {
	// Watch vector DB directory for deletion
	if chromaClient != nil {
		vectorWatcher, err := watcher.New(vectorDBPath, func() {
			log.Warn().Str("path", vectorDBPath).Msg("Vector database deleted, reconnecting ChromaDB...")
			if err := chromaClient.Reconnect(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to reconnect ChromaDB after deletion")
			}
		})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to create vector DB watcher")
		} else {
			if err := vectorWatcher.Start(); err != nil {
				log.Warn().Err(err).Msg("Failed to start vector DB watcher")
			} else {
				log.Info().Str("path", vectorDBPath).Msg("Vector DB file watcher started")
			}
		}
	}

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

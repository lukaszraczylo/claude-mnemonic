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
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/lukaszraczylo/claude-mnemonic/internal/mcp"
	"github.com/lukaszraczylo/claude-mnemonic/internal/scoring"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/internal/watcher"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
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

	// Ensure data directory and settings exist
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
	if *dataDir != "" {
		dbPath = *dataDir + "/claude-mnemonic.db"
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

	// Initialize database store (migrations run automatically)
	storeCfg := gorm.Config{
		Path:     dbPath,
		MaxConns: cfg.MaxConns,
		// WALMode is enabled automatically by GORM
	}
	store, err := gorm.NewStore(storeCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize database store")
	}
	defer store.Close()

	// Initialize stores
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	summaryStore := gorm.NewSummaryStore(store)
	promptStore := gorm.NewPromptStore(store, nil)
	patternStore := gorm.NewPatternStore(store)
	relationStore := gorm.NewRelationStore(store)
	sessionStore := gorm.NewSessionStore(store)

	// Initialize embedding service and vector client
	var vectorClient *sqlitevec.Client
	embedSvc, err := embedding.NewService()
	if err != nil {
		log.Warn().Err(err).Msg("Embedding service unavailable, vector search disabled")
	} else {
		defer embedSvc.Close()
		vectorClient, err = sqlitevec.NewClient(sqlitevec.Config{DB: store.GetRawDB()}, embedSvc)
		if err != nil {
			log.Warn().Err(err).Msg("Vector client unavailable, vector search disabled")
		} else {
			log.Info().Msg("Vector search enabled via sqlite-vec")
		}
	}

	// Initialize scoring components
	scoreConfig := models.DefaultScoringConfig()
	scoreCalculator := scoring.NewCalculator(scoreConfig)
	recalculator := scoring.NewRecalculator(observationStore, scoreCalculator, log.Logger)
	go recalculator.Start(ctx)
	defer recalculator.Stop()

	// Initialize search manager
	searchMgr := search.NewManager(observationStore, summaryStore, promptStore, vectorClient)

	// Start file watchers
	startWatchers(ctx, dbPath)

	// Create and run MCP server with all dependencies
	// Note: maintenanceService is nil because it runs in the worker process
	server := mcp.NewServer(
		searchMgr,
		Version,
		observationStore,
		patternStore,
		relationStore,
		sessionStore,
		vectorClient,
		scoreCalculator,
		recalculator,
		nil, // maintenanceService - handled by worker
	)
	log.Info().Str("project", *project).Str("version", Version).Msg("Starting MCP server")

	if err := server.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("MCP server error")
	}
}

// startWatchers initializes file watchers for config.
func startWatchers(ctx context.Context, dbPath string) {
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

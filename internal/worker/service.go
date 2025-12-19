// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/lukaszraczylo/claude-mnemonic/internal/pattern"
	"github.com/lukaszraczylo/claude-mnemonic/internal/reranking"
	"github.com/lukaszraczylo/claude-mnemonic/internal/scoring"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search/expansion"
	"github.com/lukaszraczylo/claude-mnemonic/internal/update"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/internal/watcher"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/sdk"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/session"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/sse"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Service configuration constants
const (
	// DefaultHTTPTimeout is the default timeout for HTTP requests.
	DefaultHTTPTimeout = 30 * time.Second

	// ReadyPollInterval is how often WaitReady checks initialization status.
	ReadyPollInterval = 50 * time.Millisecond

	// StaleQueueSize is the buffer size for background stale verification.
	StaleQueueSize = 100

	// QueueProcessInterval is how often the background queue processor runs.
	QueueProcessInterval = 2 * time.Second
)

// RetrievalStats tracks observation retrieval metrics.
type RetrievalStats struct {
	TotalRequests      int64 // Total retrieval requests (inject + search)
	ObservationsServed int64 // Observations returned to clients
	VerifiedStale      int64 // Stale observations that passed verification
	DeletedInvalid     int64 // Invalid observations deleted
	SearchRequests     int64 // Semantic search requests
	ContextInjections  int64 // Session-start context injections
}

// Service is the main worker service orchestrator.
type Service struct {
	// Version of the worker binary
	version string

	// Configuration
	config *config.Config

	// Database
	store            *sqlite.Store
	sessionStore     *sqlite.SessionStore
	observationStore *sqlite.ObservationStore
	summaryStore     *sqlite.SummaryStore
	promptStore      *sqlite.PromptStore
	conflictStore    *sqlite.ConflictStore
	patternStore     *sqlite.PatternStore
	relationStore    *sqlite.RelationStore

	// Pattern detection
	patternDetector *pattern.Detector

	// Domain services
	sessionManager *session.Manager
	sseBroadcaster *sse.Broadcaster
	processor      *sdk.Processor

	// Vector database (sqlite-vec with local embeddings)
	embedSvc     *embedding.Service
	vectorClient *sqlitevec.Client
	vectorSync   *sqlitevec.Sync

	// Cross-encoder reranking (for improved search relevance)
	reranker *reranking.Service

	// Query expansion (for improved search recall)
	queryExpander *expansion.Expander

	// Importance scoring
	scoreCalculator *scoring.Calculator
	recalculator    *scoring.Recalculator

	// HTTP server
	router    *chi.Mux
	server    *http.Server
	startTime time.Time

	// Retrieval statistics (per-project)
	retrievalStats   map[string]*RetrievalStats
	retrievalStatsMu sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Initialization state (for deferred init)
	ready     atomic.Bool
	initError error
	initMu    sync.RWMutex

	// Background verification queue for stale observations
	staleQueue     chan staleVerifyRequest
	staleQueueOnce sync.Once

	// File watchers for auto-recreation on deletion
	dbWatcher     *watcher.Watcher
	configWatcher *watcher.Watcher

	// Self-updater
	updater *update.Updater
}

// staleVerifyRequest represents a request to verify a stale observation in background
type staleVerifyRequest struct {
	observationID int64
	cwd           string
}

// NewService creates a new worker service with deferred initialization.
// The service starts immediately with health endpoint available,
// while database and SDK initialization happens in the background.
func NewService(version string) (*Service, error) {
	cfg := config.Get()

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Create router and SSE broadcaster (lightweight, no dependencies)
	router := chi.NewRouter()
	sseBroadcaster := sse.NewBroadcaster()

	// Determine install directory (plugin location)
	homeDir, _ := os.UserHomeDir()
	installDir := fmt.Sprintf("%s/.claude/plugins/marketplaces/claude-mnemonic", homeDir)

	svc := &Service{
		version:        version,
		config:         cfg,
		sseBroadcaster: sseBroadcaster,
		router:         router,
		ctx:            ctx,
		cancel:         cancel,
		startTime:      time.Now(),
		updater:        update.New(version, installDir),
		retrievalStats: make(map[string]*RetrievalStats),
	}

	// Setup middleware and routes (health endpoint works immediately)
	svc.setupMiddleware()
	svc.setupRoutes()

	// Start async initialization
	go svc.initializeAsync()

	return svc, nil
}

// initializeAsync performs heavy initialization in the background.
func (s *Service) initializeAsync() {
	log.Info().Msg("Starting async initialization...")

	// Ensure data directory and settings exist
	if err := config.EnsureAll(); err != nil {
		s.setInitError(fmt.Errorf("ensure data dir: %w", err))
		return
	}

	// Initialize database (this includes migrations - can be slow)
	store, err := sqlite.NewStore(sqlite.StoreConfig{
		Path:     s.config.DBPath,
		MaxConns: s.config.MaxConns,
		WALMode:  true,
	})
	if err != nil {
		s.setInitError(fmt.Errorf("init database: %w", err))
		return
	}

	// Create store wrappers
	sessionStore := sqlite.NewSessionStore(store)
	observationStore := sqlite.NewObservationStore(store)
	summaryStore := sqlite.NewSummaryStore(store)
	promptStore := sqlite.NewPromptStore(store)
	conflictStore := sqlite.NewConflictStore(store)
	patternStore := sqlite.NewPatternStore(store)
	relationStore := sqlite.NewRelationStore(store)

	// Enable conflict detection by linking stores
	observationStore.SetConflictStore(conflictStore)

	// Enable relation detection by linking stores
	observationStore.SetRelationStore(relationStore)

	// Create session manager
	sessionManager := session.NewManager(sessionStore)

	// Create embedding service and sqlite-vec client for vector search (optional)
	var embedSvc *embedding.Service
	var vectorClient *sqlitevec.Client
	var vectorSync *sqlitevec.Sync

	var reranker *reranking.Service

	emb, err := embedding.NewService()
	if err != nil {
		log.Warn().Err(err).Msg("Embedding service creation failed - vector search disabled")
	} else {
		embedSvc = emb
		// Create sqlite-vec client using the same DB connection
		client, err := sqlitevec.NewClient(sqlitevec.Config{
			DB: store.DB(),
		}, embedSvc)
		if err != nil {
			log.Warn().Err(err).Msg("sqlite-vec client creation failed - vector search disabled")
		} else {
			vectorClient = client
			vectorSync = sqlitevec.NewSync(client)
			log.Info().
				Str("model", embedSvc.Version()).
				Msg("sqlite-vec vector search enabled")
		}

		// Create cross-encoder reranking service if enabled
		if s.config.RerankingEnabled {
			rerankCfg := reranking.DefaultConfig()
			if s.config.RerankingAlpha > 0 && s.config.RerankingAlpha <= 1 {
				rerankCfg.Alpha = s.config.RerankingAlpha
			}

			ranker, err := reranking.NewService(rerankCfg)
			if err != nil {
				log.Warn().Err(err).Msg("Cross-encoder reranking service creation failed - reranking disabled")
			} else {
				reranker = ranker
				log.Info().
					Float64("alpha", rerankCfg.Alpha).
					Msg("Cross-encoder reranking enabled")
			}
		}

		// Create query expander for improved search recall
		s.queryExpander = expansion.NewExpander(embedSvc)
		log.Info().Msg("Query expansion enabled")
	}

	// Create SDK processor (optional - will be nil if Claude CLI not available)
	var processor *sdk.Processor
	proc, err := sdk.NewProcessor(observationStore, summaryStore)
	if err != nil {
		log.Warn().Err(err).Msg("SDK processor not available - observations will be queued but not processed")
	} else {
		processor = proc
		// Set broadcast callback for SSE events
		processor.SetBroadcastFunc(func(event map[string]interface{}) {
			s.sseBroadcaster.Broadcast(event)
		})
		log.Info().Msg("SDK processor initialized")
	}

	// Set all the initialized components
	s.initMu.Lock()
	s.store = store
	s.sessionStore = sessionStore
	s.observationStore = observationStore
	s.summaryStore = summaryStore
	s.promptStore = promptStore
	s.conflictStore = conflictStore
	s.patternStore = patternStore
	s.relationStore = relationStore
	s.sessionManager = sessionManager
	s.processor = processor
	s.embedSvc = embedSvc
	s.vectorClient = vectorClient
	s.vectorSync = vectorSync
	s.reranker = reranker
	s.initMu.Unlock()

	// Initialize pattern detector
	patternDetector := pattern.NewDetector(patternStore, observationStore, pattern.DefaultConfig())

	// Set pattern sync callback if vector sync is available
	if vectorSync != nil {
		patternDetector.SetSyncFunc(func(p *models.Pattern) {
			if err := vectorSync.SyncPattern(s.ctx, p); err != nil {
				log.Warn().Err(err).Int64("id", p.ID).Msg("Failed to sync pattern to sqlite-vec")
			}
		})

		// Set cleanup callback for pattern deletions
		patternStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeletePatterns(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete patterns from sqlite-vec")
			}
		})
	}

	s.initMu.Lock()
	s.patternDetector = patternDetector
	s.initMu.Unlock()

	// Set vector sync callbacks on processor if both are available
	if processor != nil && vectorSync != nil {
		processor.SetSyncObservationFunc(func(obs *models.Observation) {
			if err := vectorSync.SyncObservation(s.ctx, obs); err != nil {
				log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation to sqlite-vec")
			}
			// Trigger pattern detection for the new observation
			if patternDetector != nil {
				go func(observation *models.Observation) {
					detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if result, err := patternDetector.AnalyzeObservation(detectCtx, observation); err != nil {
						log.Warn().Err(err).Int64("obs_id", observation.ID).Msg("Pattern detection failed")
					} else if result.MatchedPattern != nil {
						log.Debug().
							Int64("pattern_id", result.MatchedPattern.ID).
							Str("pattern_name", result.MatchedPattern.Name).
							Bool("is_new", result.IsNewPattern).
							Msg("Pattern matched for observation")
					}
				}(obs)
			}
		})
		processor.SetSyncSummaryFunc(func(summary *models.SessionSummary) {
			if err := vectorSync.SyncSummary(s.ctx, summary); err != nil {
				log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary to sqlite-vec")
			}
		})
	}

	// Set cleanup callback on observation store to sync deletes to vector store
	if observationStore != nil && vectorSync != nil {
		observationStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeleteObservations(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete observations from sqlite-vec")
			}
		})
	}

	// Set cleanup callback on prompt store to sync deletes to vector store
	if promptStore != nil && vectorSync != nil {
		promptStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeleteUserPrompts(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete prompts from sqlite-vec")
			}
		})
	}

	// Set callbacks for session lifecycle events
	sessionManager.SetOnSessionCreated(func(id int64) {
		s.broadcastProcessingStatus()
		s.sseBroadcaster.Broadcast(map[string]interface{}{
			"type":   "session",
			"action": "created",
			"id":     id,
		})
	})
	sessionManager.SetOnSessionDeleted(func(id int64) {
		s.broadcastProcessingStatus()
		s.sseBroadcaster.Broadcast(map[string]interface{}{
			"type":   "session",
			"action": "deleted",
			"id":     id,
		})
	})

	// Initialize importance scoring system
	scoringConfig := models.DefaultScoringConfig()

	// Load concept weights from database if available
	if weights, err := observationStore.GetConceptWeights(s.ctx); err == nil && len(weights) > 0 {
		scoringConfig.ConceptWeights = weights
		log.Info().Int("count", len(weights)).Msg("Loaded concept weights from database")
	}

	scoreCalculator := scoring.NewCalculator(scoringConfig)
	recalculator := scoring.NewRecalculator(observationStore, scoreCalculator, log.Logger)

	s.initMu.Lock()
	s.scoreCalculator = scoreCalculator
	s.recalculator = recalculator
	s.initMu.Unlock()

	// Start background recalculator
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		recalculator.Start(s.ctx)
	}()
	log.Info().Msg("Importance scoring system initialized")

	// Start pattern detector background analysis
	if patternDetector != nil {
		patternDetector.Start()
		log.Info().Msg("Pattern recognition engine started")
	}

	// Mark as ready
	s.ready.Store(true)
	log.Info().Msg("Async initialization complete - service ready")

	// Start queue processor if SDK processor is available
	if processor != nil {
		s.wg.Add(1)
		go s.processQueue()
	}

	// Start file watchers for auto-recreation on deletion
	s.startWatchers()

	// Check if vectors need rebuilding (empty or model version mismatch) and trigger background rebuild
	if vectorClient != nil && vectorSync != nil {
		needsRebuild, reason := vectorClient.NeedsRebuild(s.ctx)
		if needsRebuild {
			log.Info().
				Str("reason", reason).
				Str("model", vectorClient.ModelVersion()).
				Msg("Vector rebuild required")

			if reason == "empty" {
				// Full rebuild - vectors table is empty
				s.wg.Add(1)
				go s.rebuildAllVectors(observationStore, summaryStore, promptStore, vectorSync)
			} else {
				// Granular rebuild - only rebuild vectors with mismatched model versions
				s.wg.Add(1)
				go s.rebuildStaleVectors(observationStore, summaryStore, promptStore, vectorClient, vectorSync)
			}
		}
	}
}

// startWatchers initializes and starts file watchers for database and config.
func (s *Service) startWatchers() {
	// Watch database file for deletion
	dbWatcher, err := watcher.New(s.config.DBPath, func() {
		log.Warn().Str("path", s.config.DBPath).Msg("Database file deleted, reinitializing...")
		s.reinitializeDatabase()
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create database watcher")
	} else {
		s.dbWatcher = dbWatcher
		if err := dbWatcher.Start(); err != nil {
			log.Warn().Err(err).Msg("Failed to start database watcher")
		} else {
			log.Info().Str("path", s.config.DBPath).Msg("Database file watcher started")
		}
	}

	// Watch config file for changes (triggers process exit for restart)
	configPath := config.SettingsPath()
	configWatcher, err := watcher.New(configPath, func() {
		log.Warn().Str("path", configPath).Msg("Config file changed, reloading...")
		s.reloadConfig()
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create config watcher")
	} else {
		s.configWatcher = configWatcher
		if err := configWatcher.Start(); err != nil {
			log.Warn().Err(err).Msg("Failed to start config watcher")
		} else {
			log.Info().Str("path", configPath).Msg("Config file watcher started")
		}
	}
}

// reinitializeDatabase recreates the database after deletion.
func (s *Service) reinitializeDatabase() {
	// Block new requests
	s.ready.Store(false)
	log.Info().Msg("Database reinitialization starting...")

	// Get old store references
	s.initMu.Lock()
	oldStore := s.store
	oldSessionManager := s.sessionManager
	oldEmbedSvc := s.embedSvc
	s.initMu.Unlock()

	// Close old embedding service
	if oldEmbedSvc != nil {
		if err := oldEmbedSvc.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing old embedding service")
		}
	}
	if oldStore != nil {
		if err := oldStore.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing old database")
		}
	}

	// Clear in-memory sessions (they reference old DB IDs)
	if oldSessionManager != nil {
		oldSessionManager.ShutdownAll(s.ctx)
	}

	// Ensure data directory and settings exist (may have been deleted)
	if err := config.EnsureAll(); err != nil {
		s.setInitError(fmt.Errorf("ensure data dir on reinit: %w", err))
		return
	}

	// Create new database
	store, err := sqlite.NewStore(sqlite.StoreConfig{
		Path:     s.config.DBPath,
		MaxConns: s.config.MaxConns,
		WALMode:  true,
	})
	if err != nil {
		s.setInitError(fmt.Errorf("reinit database: %w", err))
		return
	}

	// Create new store wrappers
	sessionStore := sqlite.NewSessionStore(store)
	observationStore := sqlite.NewObservationStore(store)
	summaryStore := sqlite.NewSummaryStore(store)
	promptStore := sqlite.NewPromptStore(store)
	conflictStore := sqlite.NewConflictStore(store)
	patternStore := sqlite.NewPatternStore(store)
	relationStore := sqlite.NewRelationStore(store)

	// Enable conflict detection by linking stores
	observationStore.SetConflictStore(conflictStore)

	// Enable relation detection by linking stores
	observationStore.SetRelationStore(relationStore)

	// Create new session manager
	sessionManager := session.NewManager(sessionStore)

	// Recreate embedding service and sqlite-vec client
	var embedSvc *embedding.Service
	var vectorClient *sqlitevec.Client
	var vectorSync *sqlitevec.Sync

	var reranker *reranking.Service

	emb, err := embedding.NewService()
	if err != nil {
		log.Warn().Err(err).Msg("Embedding service creation failed after reinit")
	} else {
		embedSvc = emb
		client, err := sqlitevec.NewClient(sqlitevec.Config{
			DB: store.DB(),
		}, embedSvc)
		if err != nil {
			log.Warn().Err(err).Msg("sqlite-vec client creation failed after reinit")
		} else {
			vectorClient = client
			vectorSync = sqlitevec.NewSync(client)
			log.Info().Msg("sqlite-vec reconnected after reinit")
		}

		// Recreate cross-encoder reranking service if enabled
		if s.config.RerankingEnabled {
			rerankCfg := reranking.DefaultConfig()
			if s.config.RerankingAlpha > 0 && s.config.RerankingAlpha <= 1 {
				rerankCfg.Alpha = s.config.RerankingAlpha
			}

			ranker, err := reranking.NewService(rerankCfg)
			if err != nil {
				log.Warn().Err(err).Msg("Cross-encoder reranking service creation failed after reinit")
			} else {
				reranker = ranker
				log.Info().Msg("Cross-encoder reranking reconnected after reinit")
			}
		}

		// Recreate query expander
		s.queryExpander = expansion.NewExpander(embedSvc)
		log.Info().Msg("Query expansion reconnected after reinit")
	}

	// Close old reranker if exists
	s.initMu.RLock()
	oldReranker := s.reranker
	s.initMu.RUnlock()
	if oldReranker != nil {
		_ = oldReranker.Close()
	}

	// Recreate SDK processor with new stores
	var processor *sdk.Processor
	proc, err := sdk.NewProcessor(observationStore, summaryStore)
	if err != nil {
		log.Warn().Err(err).Msg("SDK processor not available after reinit")
	} else {
		processor = proc
		processor.SetBroadcastFunc(func(event map[string]interface{}) {
			s.sseBroadcaster.Broadcast(event)
		})
	}

	// Stop old pattern detector if it exists
	if s.patternDetector != nil {
		s.patternDetector.Stop()
	}

	// Create new pattern detector
	patternDetector := pattern.NewDetector(patternStore, observationStore, pattern.DefaultConfig())

	// Set pattern sync callback if vector sync is available
	if vectorSync != nil {
		patternDetector.SetSyncFunc(func(p *models.Pattern) {
			if err := vectorSync.SyncPattern(s.ctx, p); err != nil {
				log.Warn().Err(err).Int64("id", p.ID).Msg("Failed to sync pattern to sqlite-vec")
			}
		})

		// Set cleanup callback for pattern deletions
		patternStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeletePatterns(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete patterns from sqlite-vec")
			}
		})
	}

	// Atomically swap all components
	s.initMu.Lock()
	s.store = store
	s.sessionStore = sessionStore
	s.observationStore = observationStore
	s.summaryStore = summaryStore
	s.promptStore = promptStore
	s.conflictStore = conflictStore
	s.patternStore = patternStore
	s.relationStore = relationStore
	s.patternDetector = patternDetector
	s.sessionManager = sessionManager
	s.processor = processor
	s.embedSvc = embedSvc
	s.vectorClient = vectorClient
	s.vectorSync = vectorSync
	s.reranker = reranker
	s.initError = nil
	s.initMu.Unlock()

	// Start pattern detector
	patternDetector.Start()

	// Set vector sync callbacks on processor if both are available
	if processor != nil && vectorSync != nil {
		processor.SetSyncObservationFunc(func(obs *models.Observation) {
			if err := vectorSync.SyncObservation(s.ctx, obs); err != nil {
				log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation to sqlite-vec")
			}
			// Trigger pattern detection for the new observation
			if patternDetector != nil {
				go func(observation *models.Observation) {
					detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if result, err := patternDetector.AnalyzeObservation(detectCtx, observation); err != nil {
						log.Warn().Err(err).Int64("obs_id", observation.ID).Msg("Pattern detection failed")
					} else if result.MatchedPattern != nil {
						log.Debug().
							Int64("pattern_id", result.MatchedPattern.ID).
							Str("pattern_name", result.MatchedPattern.Name).
							Bool("is_new", result.IsNewPattern).
							Msg("Pattern matched for observation")
					}
				}(obs)
			}
		})
		processor.SetSyncSummaryFunc(func(summary *models.SessionSummary) {
			if err := vectorSync.SyncSummary(s.ctx, summary); err != nil {
				log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary to sqlite-vec")
			}
		})
	}

	// Set cleanup callback on observation store to sync deletes to vector store
	if observationStore != nil && vectorSync != nil {
		observationStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeleteObservations(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete observations from sqlite-vec")
			}
		})
	}

	// Set cleanup callback on prompt store to sync deletes to vector store
	if promptStore != nil && vectorSync != nil {
		promptStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := vectorSync.DeleteUserPrompts(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete prompts from sqlite-vec")
			}
		})
	}

	// Set callbacks for session lifecycle events
	sessionManager.SetOnSessionCreated(func(id int64) {
		s.broadcastProcessingStatus()
		s.sseBroadcaster.Broadcast(map[string]interface{}{
			"type":   "session",
			"action": "created",
			"id":     id,
		})
	})
	sessionManager.SetOnSessionDeleted(func(id int64) {
		s.broadcastProcessingStatus()
		s.sseBroadcaster.Broadcast(map[string]interface{}{
			"type":   "session",
			"action": "deleted",
			"id":     id,
		})
	})

	// Mark as ready again
	s.ready.Store(true)
	log.Info().Msg("Database reinitialization complete")

	// Broadcast status update
	s.sseBroadcaster.Broadcast(map[string]interface{}{
		"type":    "database_reinitialized",
		"message": "Database was recreated after deletion",
	})
}

// reloadConfig reloads configuration from disk.
// For now, this triggers a graceful restart by exiting (hooks will restart us).
func (s *Service) reloadConfig() {
	log.Info().Msg("Config changed, triggering graceful restart...")

	// Broadcast notification
	s.sseBroadcaster.Broadcast(map[string]interface{}{
		"type":    "config_changed",
		"message": "Configuration changed, restarting worker...",
	})

	// Give SSE clients a moment to receive the message
	time.Sleep(100 * time.Millisecond)

	// Exit cleanly - hooks will restart us with new config
	os.Exit(0)
}

// setInitError records an initialization error.
func (s *Service) setInitError(err error) {
	s.initMu.Lock()
	s.initError = err
	s.initMu.Unlock()
	log.Error().Err(err).Msg("Async initialization failed")
}

// GetInitError returns any initialization error.
func (s *Service) GetInitError() error {
	s.initMu.RLock()
	defer s.initMu.RUnlock()
	return s.initError
}

// queueStaleVerification queues a stale observation for background verification.
// This is non-blocking - if the queue is full, the request is dropped.
func (s *Service) queueStaleVerification(observationID int64, cwd string) {
	// Initialize queue on first use
	s.staleQueueOnce.Do(func() {
		s.staleQueue = make(chan staleVerifyRequest, StaleQueueSize)
		s.wg.Add(1)
		go s.processStaleQueue()
	})

	// Non-blocking send - drop if queue is full
	select {
	case s.staleQueue <- staleVerifyRequest{observationID: observationID, cwd: cwd}:
		// Queued
	default:
		// Queue full, drop
		log.Debug().Int64("id", observationID).Msg("Stale verification queue full, dropping")
	}
}

// processStaleQueue processes stale observations in the background.
func (s *Service) processStaleQueue() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case req := <-s.staleQueue:
			s.verifyStaleObservation(req)
		}
	}
}

// rebuildAllVectors rebuilds all vectors from observations, summaries, and prompts.
// Called when the vectors table is empty (e.g., after migration 20 drops all vectors).
func (s *Service) rebuildAllVectors(
	observationStore *sqlite.ObservationStore,
	summaryStore *sqlite.SummaryStore,
	promptStore *sqlite.PromptStore,
	vectorSync *sqlitevec.Sync,
) {
	defer s.wg.Done()

	log.Info().Msg("Starting full vector rebuild...")
	start := time.Now()

	var totalSynced int
	var syncErrors int

	// Rebuild observations
	observations, err := observationStore.GetAllObservations(s.ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch observations for vector rebuild")
	} else {
		for _, obs := range observations {
			if err := vectorSync.SyncObservation(s.ctx, obs); err != nil {
				log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation during rebuild")
				syncErrors++
			} else {
				totalSynced++
			}
		}
		log.Info().Int("count", len(observations)).Msg("Rebuilt observation vectors")
	}

	// Rebuild summaries
	summaries, err := summaryStore.GetAllSummaries(s.ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch summaries for vector rebuild")
	} else {
		for _, summary := range summaries {
			if err := vectorSync.SyncSummary(s.ctx, summary); err != nil {
				log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary during rebuild")
				syncErrors++
			} else {
				totalSynced++
			}
		}
		log.Info().Int("count", len(summaries)).Msg("Rebuilt summary vectors")
	}

	// Rebuild user prompts
	prompts, err := promptStore.GetAllPrompts(s.ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch prompts for vector rebuild")
	} else {
		for _, prompt := range prompts {
			if err := vectorSync.SyncUserPrompt(s.ctx, prompt); err != nil {
				log.Warn().Err(err).Int64("id", prompt.ID).Msg("Failed to sync prompt during rebuild")
				syncErrors++
			} else {
				totalSynced++
			}
		}
		log.Info().Int("count", len(prompts)).Msg("Rebuilt prompt vectors")
	}

	elapsed := time.Since(start)
	log.Info().
		Int("total_synced", totalSynced).
		Int("errors", syncErrors).
		Dur("elapsed", elapsed).
		Msg("Full vector rebuild complete")
}

// rebuildStaleVectors rebuilds only vectors with mismatched or unknown model versions.
// This is more efficient than rebuilding all vectors when only some need updating.
func (s *Service) rebuildStaleVectors(
	observationStore *sqlite.ObservationStore,
	summaryStore *sqlite.SummaryStore,
	promptStore *sqlite.PromptStore,
	vectorClient *sqlitevec.Client,
	vectorSync *sqlitevec.Sync,
) {
	defer s.wg.Done()

	log.Info().Msg("Starting granular vector rebuild for stale vectors...")
	start := time.Now()

	// Get all stale vectors
	staleVectors, err := vectorClient.GetStaleVectors(s.ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get stale vectors")
		return
	}

	if len(staleVectors) == 0 {
		log.Info().Msg("No stale vectors found")
		return
	}

	log.Info().Int("stale_count", len(staleVectors)).Msg("Found stale vectors to rebuild")

	// Group stale vectors by doc_type and sqlite_id for efficient lookup
	staleObsIDs := make(map[int64]bool)
	staleSummaryIDs := make(map[int64]bool)
	stalePromptIDs := make(map[int64]bool)
	staleDocIDs := make([]string, 0, len(staleVectors))

	for _, sv := range staleVectors {
		staleDocIDs = append(staleDocIDs, sv.DocID)
		switch sv.DocType {
		case "observation":
			staleObsIDs[sv.SQLiteID] = true
		case "summary":
			staleSummaryIDs[sv.SQLiteID] = true
		case "prompt":
			stalePromptIDs[sv.SQLiteID] = true
		}
	}

	// Delete stale vectors before re-syncing
	if err := vectorClient.DeleteVectorsByDocIDs(s.ctx, staleDocIDs); err != nil {
		log.Error().Err(err).Msg("Failed to delete stale vectors")
		return
	}

	var totalSynced int
	var syncErrors int

	// Rebuild stale observations
	if len(staleObsIDs) > 0 {
		ids := make([]int64, 0, len(staleObsIDs))
		for id := range staleObsIDs {
			ids = append(ids, id)
		}

		observations, err := observationStore.GetObservationsByIDs(s.ctx, ids, "date_desc", 0)
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch observations for rebuild")
		} else {
			for _, obs := range observations {
				if err := vectorSync.SyncObservation(s.ctx, obs); err != nil {
					log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation during rebuild")
					syncErrors++
				} else {
					totalSynced++
				}
			}
			log.Info().Int("count", len(observations)).Msg("Rebuilt stale observation vectors")
		}
	}

	// Rebuild stale summaries
	if len(staleSummaryIDs) > 0 {
		ids := make([]int64, 0, len(staleSummaryIDs))
		for id := range staleSummaryIDs {
			ids = append(ids, id)
		}

		summaries, err := summaryStore.GetSummariesByIDs(s.ctx, ids, "date_desc", 0)
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch summaries for rebuild")
		} else {
			for _, summary := range summaries {
				if err := vectorSync.SyncSummary(s.ctx, summary); err != nil {
					log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary during rebuild")
					syncErrors++
				} else {
					totalSynced++
				}
			}
			log.Info().Int("count", len(summaries)).Msg("Rebuilt stale summary vectors")
		}
	}

	// Rebuild stale prompts
	if len(stalePromptIDs) > 0 {
		ids := make([]int64, 0, len(stalePromptIDs))
		for id := range stalePromptIDs {
			ids = append(ids, id)
		}

		prompts, err := promptStore.GetPromptsByIDs(s.ctx, ids, "date_desc", 0)
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch prompts for rebuild")
		} else {
			for _, prompt := range prompts {
				if err := vectorSync.SyncUserPrompt(s.ctx, prompt); err != nil {
					log.Warn().Err(err).Int64("id", prompt.ID).Msg("Failed to sync prompt during rebuild")
					syncErrors++
				} else {
					totalSynced++
				}
			}
			log.Info().Int("count", len(prompts)).Msg("Rebuilt stale prompt vectors")
		}
	}

	elapsed := time.Since(start)
	log.Info().
		Int("total_synced", totalSynced).
		Int("errors", syncErrors).
		Dur("elapsed", elapsed).
		Msg("Granular vector rebuild complete")
}

// verifyStaleObservation verifies a single stale observation in the background.
func (s *Service) verifyStaleObservation(req staleVerifyRequest) {
	// Wait for service to be ready
	if !s.ready.Load() {
		return
	}

	// Get observation from DB
	s.initMu.RLock()
	store := s.observationStore
	processor := s.processor
	s.initMu.RUnlock()

	if store == nil || processor == nil {
		return
	}

	obs, err := store.GetObservationByID(s.ctx, req.observationID)
	if err != nil || obs == nil {
		return
	}

	// Verify with Claude CLI (this is slow but we're in background)
	if !processor.VerifyObservation(s.ctx, obs, req.cwd) {
		// Invalid - delete it
		deleted, err := store.DeleteObservations(s.ctx, []int64{obs.ID})
		if err == nil && deleted > 0 {
			log.Info().
				Int64("id", obs.ID).
				Str("title", obs.Title.String).
				Msg("Background verification: deleted invalid observation")
		}
	} else {
		log.Debug().
			Int64("id", obs.ID).
			Msg("Background verification: observation still valid")
	}
}

// setupMiddleware configures HTTP middleware.
func (s *Service) setupMiddleware() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RealIP)
	// Note: Timeout middleware is applied per-route, not globally,
	// to avoid killing SSE connections which need to stay open indefinitely
}

// setupRoutes configures HTTP routes.
func (s *Service) setupRoutes() {
	// Serve Vue dashboard from embedded static files
	s.router.Get("/", serveIndex)
	s.router.Get("/assets/*", serveAssets)

	// Health check (both root and API-prefixed for compatibility)
	// Returns 200 immediately so hooks can connect quickly during init
	// Also returns version for stale worker detection
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/api/health", s.handleHealth)

	// Version endpoint for hooks to check if worker needs restart
	s.router.Get("/api/version", s.handleVersion)

	// Readiness check - returns 200 only when fully initialized
	s.router.Get("/api/ready", s.handleReady)

	// Update endpoints (work before DB is ready)
	s.router.Get("/api/update/check", s.handleUpdateCheck)
	s.router.Post("/api/update/apply", s.handleUpdateApply)
	s.router.Get("/api/update/status", s.handleUpdateStatus)
	s.router.Post("/api/update/restart", s.handleUpdateRestart)

	// General restart endpoint (works before DB is ready)
	s.router.Post("/api/restart", s.handleRestart)

	// Selfcheck endpoint (works before DB is ready - checks all components)
	s.router.Get("/api/selfcheck", s.handleSelfCheck)

	// SSE endpoint (works before DB is ready)
	s.router.Get("/api/events", s.sseBroadcaster.HandleSSE)

	// Routes that require DB to be ready
	s.router.Group(func(r chi.Router) {
		r.Use(s.requireReady)
		r.Use(middleware.Timeout(DefaultHTTPTimeout))

		// Session routes
		r.Post("/api/sessions/init", s.handleSessionInit)
		r.Get("/api/sessions", s.handleGetSessionByClaudeID)
		r.Post("/sessions/{id}/init", s.handleSessionStart)
		r.Post("/api/sessions/observations", s.handleObservation)
		r.Post("/api/sessions/subagent-complete", s.handleSubagentComplete)
		r.Post("/sessions/{id}/summarize", s.handleSummarize)

		// Data routes
		r.Get("/api/observations", s.handleGetObservations)
		r.Get("/api/summaries", s.handleGetSummaries)
		r.Get("/api/prompts", s.handleGetPrompts)
		r.Get("/api/projects", s.handleGetProjects)
		r.Get("/api/stats", s.handleGetStats)
		r.Get("/api/stats/retrieval", s.handleGetRetrievalStats)
		r.Get("/api/types", s.handleGetTypes)
		r.Get("/api/models", s.handleGetModels)

		// Observation scoring and feedback routes
		r.Post("/api/observations/{id}/feedback", s.handleObservationFeedback)
		r.Get("/api/observations/{id}/score", s.handleExplainScore)
		r.Get("/api/observations/top", s.handleGetTopObservations)
		r.Get("/api/observations/most-retrieved", s.handleGetMostRetrieved)

		// Scoring configuration routes
		r.Get("/api/scoring/stats", s.handleGetScoringStats)
		r.Get("/api/scoring/concepts", s.handleGetConceptWeights)
		r.Put("/api/scoring/concepts/{concept}", s.handleUpdateConceptWeight)
		r.Post("/api/scoring/recalculate", s.handleTriggerRecalculation)

		// Context injection
		r.Get("/api/context/count", s.handleContextCount)
		r.Get("/api/context/inject", s.handleContextInject)
		r.Get("/api/context/search", s.handleSearchByPrompt)

		// Pattern routes
		r.Get("/api/patterns", s.handleGetPatterns)
		r.Get("/api/patterns/stats", s.handleGetPatternStats)
		r.Get("/api/patterns/search", s.handleSearchPatterns)
		r.Get("/api/patterns/by-name", s.handleGetPatternByName)
		r.Get("/api/patterns/{id}", s.handleGetPatternByID)
		r.Get("/api/patterns/{id}/insight", s.handleGetPatternInsight)
		r.Delete("/api/patterns/{id}", s.handleDeletePattern)
		r.Post("/api/patterns/{id}/deprecate", s.handleDeprecatePattern)
		r.Post("/api/patterns/merge", s.handleMergePatterns)

		// Relation routes (knowledge graph)
		r.Get("/api/relations/stats", s.handleGetRelationStats)
		r.Get("/api/relations/type/{type}", s.handleGetRelationsByType)
		r.Get("/api/observations/{id}/relations", s.handleGetRelations)
		r.Get("/api/observations/{id}/graph", s.handleGetRelationGraph)
		r.Get("/api/observations/{id}/related", s.handleGetRelatedObservations)
	})
}

// recordRetrievalStats atomically updates retrieval statistics for a project.
func (s *Service) recordRetrievalStats(project string, served, verified, deleted int64, isSearch bool) {
	s.retrievalStatsMu.Lock()
	stats := s.retrievalStats[project]
	if stats == nil {
		stats = &RetrievalStats{}
		s.retrievalStats[project] = stats
	}
	s.retrievalStatsMu.Unlock()

	atomic.AddInt64(&stats.TotalRequests, 1)
	atomic.AddInt64(&stats.ObservationsServed, served)
	atomic.AddInt64(&stats.VerifiedStale, verified)
	atomic.AddInt64(&stats.DeletedInvalid, deleted)
	if isSearch {
		atomic.AddInt64(&stats.SearchRequests, 1)
	} else {
		atomic.AddInt64(&stats.ContextInjections, 1)
	}
}

// GetRetrievalStats returns a copy of the retrieval stats for a project.
// If project is empty, returns aggregate stats across all projects.
func (s *Service) GetRetrievalStats(project string) RetrievalStats {
	s.retrievalStatsMu.RLock()
	defer s.retrievalStatsMu.RUnlock()

	if project != "" {
		// Return stats for specific project
		stats := s.retrievalStats[project]
		if stats == nil {
			return RetrievalStats{}
		}
		return RetrievalStats{
			TotalRequests:      atomic.LoadInt64(&stats.TotalRequests),
			ObservationsServed: atomic.LoadInt64(&stats.ObservationsServed),
			VerifiedStale:      atomic.LoadInt64(&stats.VerifiedStale),
			DeletedInvalid:     atomic.LoadInt64(&stats.DeletedInvalid),
			SearchRequests:     atomic.LoadInt64(&stats.SearchRequests),
			ContextInjections:  atomic.LoadInt64(&stats.ContextInjections),
		}
	}

	// Aggregate stats across all projects
	var result RetrievalStats
	for _, stats := range s.retrievalStats {
		result.TotalRequests += atomic.LoadInt64(&stats.TotalRequests)
		result.ObservationsServed += atomic.LoadInt64(&stats.ObservationsServed)
		result.VerifiedStale += atomic.LoadInt64(&stats.VerifiedStale)
		result.DeletedInvalid += atomic.LoadInt64(&stats.DeletedInvalid)
		result.SearchRequests += atomic.LoadInt64(&stats.SearchRequests)
		result.ContextInjections += atomic.LoadInt64(&stats.ContextInjections)
	}
	return result
}

// Start starts the worker service.
// The HTTP server starts immediately; database initialization happens async.
func (s *Service) Start() error {
	port := config.GetWorkerPort()

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // Disabled for SSE (long-lived connections)
		IdleTimeout:       120 * time.Second,
	}

	// Check if we're in restart mode (after update)
	isRestart := os.Getenv("CLAUDE_MNEMONIC_RESTART") == "1"

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		var lastErr error
		maxRetries := 1
		if isRestart {
			maxRetries = 10 // Retry up to 10 times during restart
		}

		for i := 0; i < maxRetries; i++ {
			lastErr = s.server.ListenAndServe()
			if lastErr == http.ErrServerClosed {
				return // Normal shutdown
			}

			if i < maxRetries-1 && isRestart {
				log.Warn().Err(lastErr).Int("retry", i+1).Msg("Port not ready, retrying...")
				time.Sleep(500 * time.Millisecond)
				continue
			}
		}

		if lastErr != nil {
			log.Error().Err(lastErr).Msg("HTTP server error")
		}
	}()

	// Note: Queue processor is started in initializeAsync() after DB is ready

	log.Info().
		Int("port", port).
		Int("pid", getPID()).
		Bool("restart_mode", isRestart).
		Msg("Worker HTTP server started (initialization in progress)")

	return nil
}

// processQueue processes the observation queue in the background.
// Processes immediately when notified, or every QueueProcessInterval as fallback.
func (s *Service) processQueue() {
	defer s.wg.Done()

	ticker := time.NewTicker(QueueProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.sessionManager.ProcessNotify:
			// Immediate processing when observation is queued
			s.processAllSessions()
		case <-ticker.C:
			// Fallback periodic processing
			s.processAllSessions()
		}
	}
}

// processAllSessions processes pending messages for all active sessions.
// Messages are processed in parallel using goroutines, with concurrency
// limited by the processor's semaphore.
func (s *Service) processAllSessions() {
	// Get all sessions with pending messages
	sessions := s.sessionManager.GetAllSessions()

	var wg sync.WaitGroup

	for _, sess := range sessions {
		// Get pending messages
		messages := s.sessionManager.DrainMessages(sess.SessionDBID)
		if len(messages) == 0 {
			continue
		}

		// Process each message in a goroutine
		for _, msg := range messages {
			wg.Add(1)
			go func(sess *session.ActiveSession, msg session.PendingMessage) {
				defer wg.Done()

				switch msg.Type {
				case session.MessageTypeObservation:
					if msg.Observation != nil {
						err := s.processor.ProcessObservation(
							s.ctx,
							sess.SDKSessionID,
							sess.Project,
							msg.Observation.ToolName,
							msg.Observation.ToolInput,
							msg.Observation.ToolResponse,
							msg.Observation.PromptNumber,
							msg.Observation.CWD,
						)
						if err != nil {
							log.Error().Err(err).
								Str("tool", msg.Observation.ToolName).
								Msg("Failed to process observation")
						}
					}

				case session.MessageTypeSummarize:
					if msg.Summarize != nil {
						err := s.processor.ProcessSummary(
							s.ctx,
							sess.SessionDBID,
							sess.SDKSessionID,
							sess.Project,
							sess.UserPrompt,
							msg.Summarize.LastUserMessage,
							msg.Summarize.LastAssistantMessage,
						)
						if err != nil {
							log.Error().Err(err).
								Int64("sessionId", sess.SessionDBID).
								Msg("Failed to process summary")
						}
						// Delete session after summary
						s.sessionManager.DeleteSession(sess.SessionDBID)
					}
				}
			}(sess, msg)
		}
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Broadcast status after processing
	s.broadcastProcessingStatus()
}

// Shutdown gracefully shuts down the service.
func (s *Service) Shutdown(ctx context.Context) error {
	s.cancel()

	// Stop file watchers
	if s.dbWatcher != nil {
		_ = s.dbWatcher.Stop()
	}
	if s.configWatcher != nil {
		_ = s.configWatcher.Stop()
	}

	// Stop background recalculator
	if s.recalculator != nil {
		s.recalculator.Stop()
	}

	// Stop pattern detector
	if s.patternDetector != nil {
		s.patternDetector.Stop()
	}

	// Shutdown all sessions
	s.sessionManager.ShutdownAll(ctx)

	// Shutdown HTTP server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}

	// Close reranking service
	if s.reranker != nil {
		if err := s.reranker.Close(); err != nil {
			log.Error().Err(err).Msg("Reranking service close error")
		}
	}

	// Close embedding service
	if s.embedSvc != nil {
		if err := s.embedSvc.Close(); err != nil {
			log.Error().Err(err).Msg("Embedding service close error")
		}
	}

	// Close vector client
	if s.vectorClient != nil {
		if err := s.vectorClient.Close(); err != nil {
			log.Error().Err(err).Msg("Vector client close error")
		}
	}

	// Close database
	if err := s.store.Close(); err != nil {
		log.Error().Err(err).Msg("Database close error")
	}

	s.wg.Wait()

	log.Info().Msg("Worker service shutdown complete")
	return nil
}

// broadcastProcessingStatus broadcasts the current processing status.
func (s *Service) broadcastProcessingStatus() {
	isProcessing := s.sessionManager.IsAnySessionProcessing()
	queueDepth := s.sessionManager.GetTotalQueueDepth()

	s.sseBroadcaster.Broadcast(map[string]interface{}{
		"type":         "processing_status",
		"isProcessing": isProcessing,
		"queueDepth":   queueDepth,
	})
}

func getPID() int {
	return os.Getpid()
}

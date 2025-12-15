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
	"github.com/lukaszraczylo/claude-mnemonic/internal/update"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/chroma"
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

	// Domain services
	sessionManager *session.Manager
	sseBroadcaster *sse.Broadcaster
	processor      *sdk.Processor

	// Vector database
	chromaClient *chroma.Client
	chromaSync   *chroma.Sync

	// HTTP server
	router    *chi.Mux
	server    *http.Server
	startTime time.Time

	// Retrieval statistics
	retrievalStats RetrievalStats

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

	// Ensure data directory, vector-db, and settings exist
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

	// Create session manager
	sessionManager := session.NewManager(sessionStore)

	// Create ChromaDB client for vector search (optional - will be nil if unavailable)
	var chromaClient *chroma.Client
	var chromaSync *chroma.Sync
	chromaCfg := chroma.Config{
		Project:   "default", // Collection prefix
		DataDir:   s.config.VectorDBPath,
		BatchSize: 100,
	}
	client, err := chroma.NewClient(chromaCfg)
	if err != nil {
		log.Warn().Err(err).Msg("ChromaDB client creation failed - vector sync disabled")
	} else {
		// Connect to ChromaDB (starts the MCP server)
		if err := client.Connect(s.ctx); err != nil {
			log.Warn().Err(err).Msg("ChromaDB connection failed - vector sync disabled")
		} else {
			chromaClient = client
			chromaSync = chroma.NewSync(client)
			log.Info().Msg("ChromaDB client connected - vector sync enabled")
		}
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
	s.sessionManager = sessionManager
	s.processor = processor
	s.chromaClient = chromaClient
	s.chromaSync = chromaSync
	s.initMu.Unlock()

	// Set vector sync callbacks on processor if both are available
	if processor != nil && chromaSync != nil {
		processor.SetSyncObservationFunc(func(obs *models.Observation) {
			if err := chromaSync.SyncObservation(s.ctx, obs); err != nil {
				log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation to ChromaDB")
			}
		})
		processor.SetSyncSummaryFunc(func(summary *models.SessionSummary) {
			if err := chromaSync.SyncSummary(s.ctx, summary); err != nil {
				log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary to ChromaDB")
			}
		})
	}

	// Set cleanup callback on observation store to sync deletes to ChromaDB
	if observationStore != nil && chromaSync != nil {
		observationStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := chromaSync.DeleteObservations(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete observations from ChromaDB")
			}
		})
	}

	// Set cleanup callback on prompt store to sync deletes to ChromaDB
	if promptStore != nil && chromaSync != nil {
		promptStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := chromaSync.DeleteUserPrompts(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete prompts from ChromaDB")
			}
		})
	}

	// Set callback for session deletion
	sessionManager.SetOnSessionDeleted(func(id int64) {
		s.broadcastProcessingStatus()
	})

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
	oldChromaClient := s.chromaClient
	s.initMu.Unlock()

	// Close old stores
	if oldChromaClient != nil {
		if err := oldChromaClient.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing old ChromaDB client")
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

	// Ensure data directory, vector-db, and settings exist (may have been deleted)
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

	// Create new session manager
	sessionManager := session.NewManager(sessionStore)

	// Recreate ChromaDB client
	var chromaClient *chroma.Client
	var chromaSync *chroma.Sync
	chromaCfg := chroma.Config{
		Project:   "default",
		DataDir:   s.config.VectorDBPath,
		BatchSize: 100,
	}
	client, err := chroma.NewClient(chromaCfg)
	if err != nil {
		log.Warn().Err(err).Msg("ChromaDB client creation failed after reinit")
	} else {
		if err := client.Connect(s.ctx); err != nil {
			log.Warn().Err(err).Msg("ChromaDB connection failed after reinit")
		} else {
			chromaClient = client
			chromaSync = chroma.NewSync(client)
			log.Info().Msg("ChromaDB client reconnected after reinit")
		}
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

	// Atomically swap all components
	s.initMu.Lock()
	s.store = store
	s.sessionStore = sessionStore
	s.observationStore = observationStore
	s.summaryStore = summaryStore
	s.promptStore = promptStore
	s.sessionManager = sessionManager
	s.processor = processor
	s.chromaClient = chromaClient
	s.chromaSync = chromaSync
	s.initError = nil
	s.initMu.Unlock()

	// Set vector sync callbacks on processor if both are available
	if processor != nil && chromaSync != nil {
		processor.SetSyncObservationFunc(func(obs *models.Observation) {
			if err := chromaSync.SyncObservation(s.ctx, obs); err != nil {
				log.Warn().Err(err).Int64("id", obs.ID).Msg("Failed to sync observation to ChromaDB")
			}
		})
		processor.SetSyncSummaryFunc(func(summary *models.SessionSummary) {
			if err := chromaSync.SyncSummary(s.ctx, summary); err != nil {
				log.Warn().Err(err).Int64("id", summary.ID).Msg("Failed to sync summary to ChromaDB")
			}
		})
	}

	// Set cleanup callback on observation store to sync deletes to ChromaDB
	if observationStore != nil && chromaSync != nil {
		observationStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := chromaSync.DeleteObservations(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete observations from ChromaDB")
			}
		})
	}

	// Set cleanup callback on prompt store to sync deletes to ChromaDB
	if promptStore != nil && chromaSync != nil {
		promptStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
			if err := chromaSync.DeleteUserPrompts(ctx, deletedIDs); err != nil {
				log.Warn().Err(err).Ints64("ids", deletedIDs).Msg("Failed to delete prompts from ChromaDB")
			}
		})
	}

	// Set callback for session deletion
	sessionManager.SetOnSessionDeleted(func(id int64) {
		s.broadcastProcessingStatus()
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
	s.router.Use(middleware.Timeout(DefaultHTTPTimeout))
	s.router.Use(middleware.RealIP)
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

	// Selfcheck endpoint (works before DB is ready - checks all components)
	s.router.Get("/api/selfcheck", s.handleSelfCheck)

	// SSE endpoint (works before DB is ready)
	s.router.Get("/api/events", s.sseBroadcaster.HandleSSE)

	// Routes that require DB to be ready
	s.router.Group(func(r chi.Router) {
		r.Use(s.requireReady)

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

		// Context injection
		r.Get("/api/context/count", s.handleContextCount)
		r.Get("/api/context/inject", s.handleContextInject)
		r.Get("/api/context/search", s.handleSearchByPrompt)
	})
}

// recordRetrievalStats atomically updates retrieval statistics.
func (s *Service) recordRetrievalStats(served, verified, deleted int64, isSearch bool) {
	atomic.AddInt64(&s.retrievalStats.TotalRequests, 1)
	atomic.AddInt64(&s.retrievalStats.ObservationsServed, served)
	atomic.AddInt64(&s.retrievalStats.VerifiedStale, verified)
	atomic.AddInt64(&s.retrievalStats.DeletedInvalid, deleted)
	if isSearch {
		atomic.AddInt64(&s.retrievalStats.SearchRequests, 1)
	} else {
		atomic.AddInt64(&s.retrievalStats.ContextInjections, 1)
	}
}

// GetRetrievalStats returns a copy of the retrieval stats.
func (s *Service) GetRetrievalStats() RetrievalStats {
	return RetrievalStats{
		TotalRequests:      atomic.LoadInt64(&s.retrievalStats.TotalRequests),
		ObservationsServed: atomic.LoadInt64(&s.retrievalStats.ObservationsServed),
		VerifiedStale:      atomic.LoadInt64(&s.retrievalStats.VerifiedStale),
		DeletedInvalid:     atomic.LoadInt64(&s.retrievalStats.DeletedInvalid),
		SearchRequests:     atomic.LoadInt64(&s.retrievalStats.SearchRequests),
		ContextInjections:  atomic.LoadInt64(&s.retrievalStats.ContextInjections),
	}
}

// Start starts the worker service.
// The HTTP server starts immediately; database initialization happens async.
func (s *Service) Start() error {
	port := config.GetWorkerPort()

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
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
func (s *Service) processQueue() {
	defer s.wg.Done()

	ticker := time.NewTicker(QueueProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processAllSessions()
		}
	}
}

// processAllSessions processes pending messages for all active sessions.
func (s *Service) processAllSessions() {
	// Get all sessions with pending messages
	sessions := s.sessionManager.GetAllSessions()

	for _, sess := range sessions {
		// Get pending messages
		messages := s.sessionManager.DrainMessages(sess.SessionDBID)
		if len(messages) == 0 {
			continue
		}

		// Process each message
		for _, msg := range messages {
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
		}

		s.broadcastProcessingStatus()
	}
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

	// Shutdown all sessions
	s.sessionManager.ShutdownAll(ctx)

	// Shutdown HTTP server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}

	// Close ChromaDB client
	if s.chromaClient != nil {
		if err := s.chromaClient.Close(); err != nil {
			log.Error().Err(err).Msg("ChromaDB close error")
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

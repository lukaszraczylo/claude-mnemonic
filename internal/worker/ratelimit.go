// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	lastUpdate time.Time
	rate       float64
	burst      int
	tokens     float64
	requests   int64
	rejected   int64
	mu         sync.Mutex
}

// LastUpdateTime returns the last update time.
// Thread-safe - acquires the limiter's lock.
func (rl *RateLimiter) LastUpdateTime() time.Time {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.lastUpdate
}

// lastUpdateTimeUnlocked returns the last update time without locking.
// Caller must hold rl.mu.
func (rl *RateLimiter) lastUpdateTimeUnlocked() time.Time {
	return rl.lastUpdate
}

// NewRateLimiter creates a new rate limiter.
// rate is the number of requests per second to allow.
// burst is the maximum burst of requests to allow.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

// Allow checks if a request should be allowed.
// Returns true if the request is allowed, false if rate limited.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.requests++

	// Calculate tokens added since last update
	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > float64(rl.burst) {
		rl.tokens = float64(rl.burst)
	}
	rl.lastUpdate = now

	// Check if we have a token available
	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}

	rl.rejected++
	return false
}

// Stats returns rate limiter statistics.
func (rl *RateLimiter) Stats() map[string]any {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return map[string]any{
		"rate":           rl.rate,
		"burst":          rl.burst,
		"current_tokens": rl.tokens,
		"total_requests": rl.requests,
		"rejected":       rl.rejected,
		"rejection_rate": float64(rl.rejected) / max(float64(rl.requests), 1),
	}
}

// RateLimitMiddleware creates middleware that applies rate limiting.
// Uses a shared rate limiter for all requests.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PerClientRateLimiter implements per-client rate limiting.
type PerClientRateLimiter struct {
	lastCleanup     time.Time
	clients         map[string]*RateLimiter
	rate            float64
	burst           int
	cleanupInterval time.Duration
	maxIdleTime     time.Duration
	mu              sync.Mutex
}

// NewPerClientRateLimiter creates a new per-client rate limiter.
func NewPerClientRateLimiter(rate float64, burst int) *PerClientRateLimiter {
	return &PerClientRateLimiter{
		rate:            rate,
		burst:           burst,
		clients:         make(map[string]*RateLimiter),
		cleanupInterval: 5 * time.Minute,
		maxIdleTime:     10 * time.Minute,
		lastCleanup:     time.Now(),
	}
}

// getLimiter returns a rate limiter for the given client key.
func (pcrl *PerClientRateLimiter) getLimiter(key string) *RateLimiter {
	pcrl.mu.Lock()
	defer pcrl.mu.Unlock()

	// Periodic cleanup of idle clients
	if time.Since(pcrl.lastCleanup) > pcrl.cleanupInterval {
		pcrl.cleanupLocked()
	}

	limiter, exists := pcrl.clients[key]
	if !exists {
		limiter = NewRateLimiter(pcrl.rate, pcrl.burst)
		pcrl.clients[key] = limiter
	}

	return limiter
}

// cleanupLocked removes idle limiters. Must be called with lock held.
// Uses consistent lock ordering: always acquire limiter.mu while holding pcrl.mu.
// This is safe because the limiter.mu critical section is brief (just reading lastUpdate).
func (pcrl *PerClientRateLimiter) cleanupLocked() {
	now := time.Now()
	keysToDelete := make([]string, 0)

	// Check each limiter while holding pcrl.mu
	// We briefly acquire limiter.mu but the critical section is minimal
	for key, limiter := range pcrl.clients {
		limiter.mu.Lock()
		lastUpdate := limiter.lastUpdateTimeUnlocked()
		limiter.mu.Unlock()

		if now.Sub(lastUpdate) > pcrl.maxIdleTime {
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Delete collected keys
	for _, key := range keysToDelete {
		delete(pcrl.clients, key)
	}
	pcrl.lastCleanup = now
}

// Allow checks if a request from the given client should be allowed.
func (pcrl *PerClientRateLimiter) Allow(clientKey string) bool {
	return pcrl.getLimiter(clientKey).Allow()
}

// Stats returns aggregate statistics.
// Uses two-phase approach to avoid nested lock acquisition.
func (pcrl *PerClientRateLimiter) Stats() map[string]any {
	// Phase 1: Collect limiters under pcrl.mu
	pcrl.mu.Lock()
	rate := pcrl.rate
	burst := pcrl.burst
	activeClients := len(pcrl.clients)
	limiters := make([]*RateLimiter, 0, activeClients)
	for _, limiter := range pcrl.clients {
		limiters = append(limiters, limiter)
	}
	pcrl.mu.Unlock()

	// Phase 2: Collect stats from each limiter (only acquiring limiter.mu, not pcrl.mu)
	var totalRequests, totalRejected int64
	for _, limiter := range limiters {
		limiter.mu.Lock()
		totalRequests += limiter.requests
		totalRejected += limiter.rejected
		limiter.mu.Unlock()
	}

	return map[string]any{
		"rate":           rate,
		"burst":          burst,
		"active_clients": activeClients,
		"total_requests": totalRequests,
		"total_rejected": totalRejected,
	}
}

// PerClientRateLimitMiddleware creates middleware that applies per-client rate limiting.
// Uses X-Forwarded-For or RemoteAddr to identify clients.
func PerClientRateLimitMiddleware(limiter *PerClientRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client identifier (prefer X-Real-IP from RealIP middleware)
			clientKey := r.RemoteAddr
			if xff := r.Header.Get("X-Real-IP"); xff != "" {
				clientKey = xff
			}

			if !limiter.Allow(clientKey) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

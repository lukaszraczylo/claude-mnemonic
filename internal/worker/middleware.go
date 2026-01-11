// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// requestIDKey is the context key for request IDs.
type requestIDKey struct{}

// projectNamePattern validates project names to prevent path traversal.
var projectNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)

// allowedOrigins is the whitelist of origins allowed for CORS.
// Uses exact matching to prevent bypass attacks like "evil-localhost.com".
var allowedOrigins = map[string]bool{
	"http://localhost":       true,
	"http://localhost:3000":  true,
	"http://localhost:5173":  true, // Vite dev server
	"http://localhost:37778": true, // Dashboard UI
	"http://127.0.0.1":       true,
	"http://127.0.0.1:3000":  true,
	"http://127.0.0.1:5173":  true,
	"http://127.0.0.1:37778": true,
}

// SecurityHeaders middleware adds essential security headers to all responses.
// These protect against common web vulnerabilities.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Enable XSS filter
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Restrict referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy - restrict to self
		w.Header().Set("Content-Security-Policy", "default-src 'self'")

		// Permissions Policy - disable unnecessary features
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// CORS: Use exact match whitelist to prevent bypass attacks
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token, Authorization, X-Request-ID")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MaxBodySize middleware limits the size of incoming request bodies.
// This prevents denial of service attacks via large payloads.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// TokenAuth provides simple token-based authentication for localhost services.
// The token is generated at startup and must be provided in the X-Auth-Token header.
type TokenAuth struct {
	ExemptPaths map[string]bool
	token       string
	mu          sync.RWMutex
	enabled     bool
}

// NewTokenAuth creates a new TokenAuth with a randomly generated token.
// If enabled is false, authentication is skipped (useful for development).
func NewTokenAuth(enabled bool) (*TokenAuth, error) {
	ta := &TokenAuth{
		enabled: enabled,
		ExemptPaths: map[string]bool{
			"/health":     true,
			"/api/health": true,
			"/api/ready":  true,
		},
	}

	if enabled {
		// Generate 32-byte random token
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return nil, err
		}
		ta.token = hex.EncodeToString(tokenBytes)
	}

	return ta, nil
}

// Token returns the authentication token.
// Returns empty string if authentication is disabled.
func (ta *TokenAuth) Token() string {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	return ta.token
}

// IsEnabled returns whether token authentication is enabled.
func (ta *TokenAuth) IsEnabled() bool {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	return ta.enabled
}

// Middleware returns HTTP middleware that enforces token authentication.
func (ta *TokenAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ta.mu.RLock()
		enabled := ta.enabled
		token := ta.token
		exempt := ta.ExemptPaths[r.URL.Path]
		ta.mu.RUnlock()

		// Skip auth if disabled or path is exempt
		if !enabled || exempt {
			next.ServeHTTP(w, r)
			return
		}

		// Check for token in header
		providedToken := r.Header.Get("X-Auth-Token")
		if providedToken == "" {
			// Also check Authorization header with Bearer scheme
			auth := r.Header.Get("Authorization")
			if bearer, found := strings.CutPrefix(auth, "Bearer "); found {
				providedToken = bearer
			}
		}

		if providedToken != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ExpensiveOperationLimiter provides stricter rate limiting for expensive operations.
// It wraps the base per-client rate limiter with additional per-operation limits.
type ExpensiveOperationLimiter struct {
	// Track last execution time per operation type
	lastRebuild     int64 // Unix timestamp
	rebuildCooldown int64 // Minimum seconds between rebuilds

	mu sync.Mutex
}

// NewExpensiveOperationLimiter creates a limiter for expensive operations.
func NewExpensiveOperationLimiter() *ExpensiveOperationLimiter {
	return &ExpensiveOperationLimiter{
		rebuildCooldown: 300, // 5 minutes between rebuilds
	}
}

// CanRebuild checks if a vector rebuild operation is allowed.
// Returns false if a rebuild was triggered too recently.
func (eol *ExpensiveOperationLimiter) CanRebuild() bool {
	eol.mu.Lock()
	defer eol.mu.Unlock()

	now := unixNow()
	if now-eol.lastRebuild < eol.rebuildCooldown {
		return false
	}
	eol.lastRebuild = now
	return true
}

// unixNow returns current Unix timestamp.
// Separated for easier testing.
func unixNow() int64 {
	return time.Now().Unix()
}

// RequestID middleware adds a unique request ID to each request.
// The ID is added to the context and response headers for tracing.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for existing request ID from client
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			// Generate new request ID
			idBytes := make([]byte, 8)
			if _, err := rand.Read(idBytes); err == nil {
				requestID = hex.EncodeToString(idBytes)
			} else {
				requestID = fmt.Sprintf("%d", time.Now().UnixNano())
			}
		}

		// Add to response header
		w.Header().Set("X-Request-ID", requestID)

		// Add to context
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// RequireJSONContentType middleware validates that POST/PUT/PATCH requests
// have application/json Content-Type header.
func RequireJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check for methods that typically have bodies
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			ct := r.Header.Get("Content-Type")
			// Allow empty Content-Type for requests without body
			if ct != "" && !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateProjectName checks if a project name is safe to use.
// Returns an error if the name contains path traversal or invalid characters.
func ValidateProjectName(project string) error {
	if project == "" {
		return nil // Empty is allowed (means no filter)
	}

	// Check for path traversal
	if strings.Contains(project, "..") {
		return fmt.Errorf("invalid project name: path traversal detected")
	}

	// Check for valid characters
	if !projectNamePattern.MatchString(project) {
		return fmt.Errorf("invalid project name: only alphanumeric, underscore, dash, dot, and slash allowed")
	}

	// Max length check
	if len(project) > 500 {
		return fmt.Errorf("project name too long (max 500 chars)")
	}

	return nil
}

// BulkOperationLimiter provides rate limiting for bulk operations.
// Prevents DoS via repeated bulk requests.
type BulkOperationLimiter struct {
	lastBulkOp int64 // Unix timestamp
	cooldown   int64 // Minimum seconds between operations

	mu sync.Mutex
}

// NewBulkOperationLimiter creates a limiter for bulk operations.
func NewBulkOperationLimiter(cooldownSeconds int64) *BulkOperationLimiter {
	return &BulkOperationLimiter{
		cooldown: cooldownSeconds,
	}
}

// CanExecute checks if a bulk operation is allowed.
// Returns false if a bulk operation was triggered too recently.
func (bol *BulkOperationLimiter) CanExecute() bool {
	bol.mu.Lock()
	defer bol.mu.Unlock()

	now := unixNow()
	if now-bol.lastBulkOp < bol.cooldown {
		return false
	}
	bol.lastBulkOp = now
	return true
}

// TimeSinceLastOp returns seconds since the last bulk operation.
func (bol *BulkOperationLimiter) TimeSinceLastOp() int64 {
	bol.mu.Lock()
	defer bol.mu.Unlock()
	return unixNow() - bol.lastBulkOp
}

// CooldownRemaining returns seconds remaining in the cooldown period.
// Returns 0 if no cooldown is active.
func (bol *BulkOperationLimiter) CooldownRemaining() int64 {
	bol.mu.Lock()
	defer bol.mu.Unlock()

	remaining := bol.cooldown - (unixNow() - bol.lastBulkOp)
	if remaining < 0 {
		return 0
	}
	return remaining
}

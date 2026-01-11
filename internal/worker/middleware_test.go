package worker

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check all security headers are set
	tests := []struct {
		header   string
		expected string
	}{
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		if got := rr.Header().Get(tt.header); got != tt.expected {
			t.Errorf("SecurityHeaders() %s = %q, want %q", tt.header, got, tt.expected)
		}
	}
}

func TestSecurityHeaders_CORS(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		origin         string
		expectedOrigin string
		expectCORS     bool
	}{
		{
			name:           "localhost:37778 origin allowed",
			origin:         "http://localhost:37778",
			expectCORS:     true,
			expectedOrigin: "http://localhost:37778",
		},
		{
			name:           "127.0.0.1:5173 origin allowed",
			origin:         "http://127.0.0.1:5173",
			expectCORS:     true,
			expectedOrigin: "http://127.0.0.1:5173",
		},
		{
			name:           "localhost without port allowed",
			origin:         "http://localhost",
			expectCORS:     true,
			expectedOrigin: "http://localhost",
		},
		{
			name:       "external origin blocked",
			origin:     "http://evil.com",
			expectCORS: false,
		},
		{
			name:       "evil-localhost.com bypass attempt blocked",
			origin:     "http://evil-localhost.com",
			expectCORS: false,
		},
		{
			name:       "localhost subdomain bypass attempt blocked",
			origin:     "http://localhost.evil.com",
			expectCORS: false,
		},
		{
			name:       "unknown localhost port blocked",
			origin:     "http://localhost:9999",
			expectCORS: false,
		},
		{
			name:       "no origin header",
			origin:     "",
			expectCORS: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			cors := rr.Header().Get("Access-Control-Allow-Origin")
			if tt.expectCORS {
				if cors != tt.expectedOrigin {
					t.Errorf("Expected CORS origin %q, got %q", tt.expectedOrigin, cors)
				}
			} else {
				if cors != "" {
					t.Errorf("Expected no CORS header, got %q", cors)
				}
			}
		})
	}
}

func TestMaxBodySize(t *testing.T) {
	maxSize := int64(100)
	handler := MaxBodySize(maxSize)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		contentLength  int64
		expectedStatus int
	}{
		{
			name:           "within limit",
			contentLength:  50,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "at limit",
			contentLength:  100,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "exceeds limit",
			contentLength:  150,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", nil)
			req.ContentLength = tt.contentLength
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("MaxBodySize() status = %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestTokenAuth(t *testing.T) {
	t.Run("disabled auth allows all requests", func(t *testing.T) {
		ta, err := NewTokenAuth(false)
		if err != nil {
			t.Fatalf("NewTokenAuth() error = %v", err)
		}

		handler := ta.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected OK with disabled auth, got %d", rr.Code)
		}
	})

	t.Run("enabled auth requires token", func(t *testing.T) {
		ta, err := NewTokenAuth(true)
		if err != nil {
			t.Fatalf("NewTokenAuth() error = %v", err)
		}

		handler := ta.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Without token
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected Unauthorized without token, got %d", rr.Code)
		}

		// With correct token in X-Auth-Token header
		req = httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Auth-Token", ta.Token())
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected OK with correct token, got %d", rr.Code)
		}

		// With correct token in Authorization header
		req = httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+ta.Token())
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected OK with Bearer token, got %d", rr.Code)
		}
	})

	t.Run("exempt paths skip auth", func(t *testing.T) {
		ta, err := NewTokenAuth(true)
		if err != nil {
			t.Fatalf("NewTokenAuth() error = %v", err)
		}

		handler := ta.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		exemptPaths := []string{"/health", "/api/health", "/api/ready"}
		for _, path := range exemptPaths {
			req := httptest.NewRequest("GET", path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected OK for exempt path %s, got %d", path, rr.Code)
			}
		}
	})
}

func TestExpensiveOperationLimiter(t *testing.T) {
	limiter := NewExpensiveOperationLimiter()

	// First rebuild should be allowed
	if !limiter.CanRebuild() {
		t.Error("First rebuild should be allowed")
	}

	// Immediate second rebuild should be blocked
	if limiter.CanRebuild() {
		t.Error("Immediate second rebuild should be blocked")
	}
}

func TestRequestID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request ID is in context
		id := GetRequestID(r.Context())
		if id == "" {
			t.Error("Request ID should be set in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("generates new request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Header().Get("X-Request-ID") == "" {
			t.Error("X-Request-ID header should be set")
		}
	})

	t.Run("uses existing request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Request-ID", "test-id-12345")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Header().Get("X-Request-ID") != "test-id-12345" {
			t.Errorf("Expected X-Request-ID to be test-id-12345, got %s", rr.Header().Get("X-Request-ID"))
		}
	})
}

func TestRequireJSONContentType(t *testing.T) {
	handler := RequireJSONContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		method         string
		contentType    string
		expectedStatus int
	}{
		{
			name:           "GET request without content-type",
			method:         "GET",
			contentType:    "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST with application/json",
			method:         "POST",
			contentType:    "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST with application/json; charset=utf-8",
			method:         "POST",
			contentType:    "application/json; charset=utf-8",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST without content-type (empty body)",
			method:         "POST",
			contentType:    "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST with text/plain rejected",
			method:         "POST",
			contentType:    "text/plain",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "PUT with application/xml rejected",
			method:         "PUT",
			contentType:    "application/xml",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "PATCH with form-urlencoded rejected",
			method:         "PATCH",
			contentType:    "application/x-www-form-urlencoded",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		wantError bool
	}{
		{
			name:      "empty project allowed",
			project:   "",
			wantError: false,
		},
		{
			name:      "simple project name",
			project:   "my-project",
			wantError: false,
		},
		{
			name:      "project with path",
			project:   "org/my-project",
			wantError: false,
		},
		{
			name:      "project with underscore",
			project:   "my_project_v2",
			wantError: false,
		},
		{
			name:      "project with dot",
			project:   "my.project.name",
			wantError: false,
		},
		{
			name:      "path traversal attack",
			project:   "../../../etc/passwd",
			wantError: true,
		},
		{
			name:      "hidden path traversal",
			project:   "project/../../secret",
			wantError: true,
		},
		{
			name:      "shell injection attempt",
			project:   "project; rm -rf /",
			wantError: true,
		},
		{
			name:      "backtick injection",
			project:   "project`whoami`",
			wantError: true,
		},
		{
			name:      "special characters",
			project:   "project$HOME",
			wantError: true,
		},
		{
			name:      "too long project name",
			project:   string(make([]byte, 501)),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectName(tt.project)
			if tt.wantError && err == nil {
				t.Errorf("Expected error for project %q, got nil", tt.project)
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error for project %q: %v", tt.project, err)
			}
		})
	}
}

func TestBulkOperationLimiter(t *testing.T) {
	limiter := NewBulkOperationLimiter(1) // 1 second cooldown for testing

	// First operation should be allowed
	if !limiter.CanExecute() {
		t.Error("First bulk operation should be allowed")
	}

	// Immediate second operation should be blocked
	if limiter.CanExecute() {
		t.Error("Immediate second bulk operation should be blocked")
	}

	// Check cooldown remaining
	remaining := limiter.CooldownRemaining()
	if remaining <= 0 || remaining > 1 {
		t.Errorf("Expected cooldown remaining between 0-1 seconds, got %d", remaining)
	}

	// Check time since last op
	since := limiter.TimeSinceLastOp()
	if since < 0 || since > 1 {
		t.Errorf("Expected time since last op between 0-1 seconds, got %d", since)
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check CSP header is set
	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header should be set")
	}
	if csp != "default-src 'self'" {
		t.Errorf("Expected CSP to be \"default-src 'self'\", got %q", csp)
	}

	// Check Permissions-Policy header
	pp := rr.Header().Get("Permissions-Policy")
	if pp == "" {
		t.Error("Permissions-Policy header should be set")
	}
}

func TestSecurityHeaders_Preflight(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// OPTIONS should return 204 No Content
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status 204 for OPTIONS, got %d", rr.Code)
	}

	// CORS headers should be set for allowed origin
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("CORS origin should be set for allowed origin")
	}
	if rr.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Access-Control-Allow-Methods should be set")
	}
}

package worker

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

//go:embed static/*
var staticFS embed.FS

// staticFiles lazily resolves the embedded "static" subdirectory exactly once.
// Using sync.OnceValues avoids an init() and package-level mutable state while
// preserving the previous behavior: the dashboard is served when resolution
// succeeds and reports a clear error otherwise.
var staticFiles = sync.OnceValues(func() (fs.FS, error) {
	return fs.Sub(staticFS, "static")
})

// serveIndex serves the index.html file for the root path
func serveIndex(w http.ResponseWriter, r *http.Request) {
	sub, err := staticFiles()
	if err != nil {
		http.Error(w, "Dashboard unavailable: static files not initialized", http.StatusServiceUnavailable)
		return
	}
	content, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(content)
}

// serveAssets serves static assets from the embedded filesystem
func serveAssets(w http.ResponseWriter, r *http.Request) {
	sub, err := staticFiles()
	if err != nil {
		http.Error(w, "Assets unavailable: static files not initialized", http.StatusServiceUnavailable)
		return
	}
	// Strip the /assets/ prefix and serve the file
	path := strings.TrimPrefix(r.URL.Path, "/")

	content, err := fs.ReadFile(sub, path)
	if err != nil {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}

	// Set content type based on extension
	if strings.HasSuffix(path, ".js") {
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	}

	// No caching - always serve fresh content
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(content)
}

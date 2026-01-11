package worker

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

//go:embed static/*
var staticFS embed.FS

// staticSubFS is the static subdirectory filesystem
var staticSubFS fs.FS

// staticInitErr stores any error from static filesystem initialization
var staticInitErr error

func init() {
	staticSubFS, staticInitErr = fs.Sub(staticFS, "static")
	if staticInitErr != nil {
		log.Warn().Err(staticInitErr).Msg("Static filesystem initialization failed - dashboard will be unavailable")
	}
}

// serveIndex serves the index.html file for the root path
func serveIndex(w http.ResponseWriter, r *http.Request) {
	if staticInitErr != nil {
		http.Error(w, "Dashboard unavailable: static files not initialized", http.StatusServiceUnavailable)
		return
	}
	content, err := fs.ReadFile(staticSubFS, "index.html")
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
	if staticInitErr != nil {
		http.Error(w, "Assets unavailable: static files not initialized", http.StatusServiceUnavailable)
		return
	}
	// Strip the /assets/ prefix and serve the file
	path := strings.TrimPrefix(r.URL.Path, "/")

	content, err := fs.ReadFile(staticSubFS, path)
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

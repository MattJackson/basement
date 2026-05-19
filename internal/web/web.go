// Package web serves the embedded SPA and static assets.
package web

import (
	"embed"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path

		ext := strings.ToLower(filepath.Ext(path))
		supportedExtensions := map[string]bool{
			".js":    true,
			".css":   true,
			".svg":   true,
			".png":   true,
			".ico":   true,
			".woff":  true,
			".woff2": true,
			".jpg":   true,
			".jpeg":  true,
			".webp":  true,
			".map":   true,
			".json":  true,
		}

		if supportedExtensions[ext] {
			serveFromAssets(w, r)
			return
		}

		indexHTML, err := distFS.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(indexHTML)
	})
}

func serveFromAssets(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/assets/")
	if path == "" || path == "/" {
		http.NotFound(w, r)
		return
	}

	fullPath := "dist/assets/" + path

	data, err := distFS.ReadFile(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := http.DetectContentType(data)
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// Package web serves the embedded SPA and static assets.
package web

import (
	"bytes"
	"embed"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist placeholder.html
var distFS embed.FS

// Common SPA asset MIME types. Registered with Go's mime package on
// init() so http.ServeContent + mime.TypeByExtension return the
// browser-expected Content-Type even on systems with a sparse mime DB
// (e.g. distroless/scratch containers).
var assetMIME = map[string]string{
	".js":          "application/javascript; charset=utf-8",
	".mjs":         "application/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".map":         "application/json; charset=utf-8",
	".webmanifest": "application/manifest+json; charset=utf-8",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml; charset=utf-8",
}

func init() {
	for ext, ct := range assetMIME {
		_ = mime.AddExtensionType(ext, ct)
	}
}

// Handler returns the HTTP handler for serving the embedded SPA + assets.
//
// /assets/* serves files from dist/assets/ with extension-based MIME
// (NEVER content-sniffed — Go's http.DetectContentType returns
// "text/plain" for minified JS, which Chromium refuses to execute
// under strict module-script MIME checking).
//
// Everything else serves dist/index.html (SPA fallback) so client-side
// routing works. Falls back to placeholder.html if dist/index.html is
// missing (fresh clone before pnpm build).
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/assets/") {
			serveAsset(w, r)
			return
		}

		// Bare asset path at root (favicon.svg, icons.svg, etc.)
		ext := strings.ToLower(path.Ext(r.URL.Path))
		if _, ok := assetMIME[ext]; ok {
			serveAsset(w, r)
			return
		}

		serveSPAIndex(w, r)
	})
}

func serveAsset(w http.ResponseWriter, r *http.Request) {
	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	if urlPath == "" {
		http.NotFound(w, r)
		return
	}

	fullPath := "dist/" + urlPath

	data, err := distFS.ReadFile(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(path.Ext(urlPath))
	contentType := assetMIME[ext]
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)

	// /assets/* are content-hashed by Vite and safe to immutable-cache
	// for a year. Root-level assets (favicon.svg, site.webmanifest,
	// og-image.png, apple-touch-icon.png, etc.) are stable paths whose
	// CONTENT changes between releases — caching them immutable wedges
	// stale icons on users for a year. Cache them short with
	// revalidation instead. See the 2026-05-19 favicon incident.
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
	}

	// http.ServeContent handles Range, If-Modified-Since, HEAD properly.
	http.ServeContent(w, r, urlPath, modTime(fullPath), bytes.NewReader(data))
}

func serveSPAIndex(w http.ResponseWriter, r *http.Request) {
	indexHTML, err := distFS.ReadFile("dist/index.html")
	if err != nil {
		indexHTML, err = distFS.ReadFile("placeholder.html")
		if err != nil {
			http.Error(w, "no SPA assets available", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// no-store on the entry HTML so the browser never serves a stale
	// reference to old hashed bundles after a deploy — even out of
	// bfcache. The HTML is tiny (~2KB) and HTTP/2-multiplexed, so the
	// extra round-trip is negligible. Paired with X-Build header on
	// /api/v1 responses + version-mismatch banner in the frontend.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(indexHTML)
}

// modTime returns the embedded file's modification time, or zero if
// unavailable. Used for If-Modified-Since handling via http.ServeContent.
func modTime(fullPath string) time.Time {
	f, err := distFS.Open(fullPath)
	if err != nil {
		return time.Time{}
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

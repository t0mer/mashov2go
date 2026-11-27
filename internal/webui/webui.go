// Package webui serves the admin UI, which is built by Vite from web/ into dist/
// and embedded in the binary (CLAUDE.md §10, guidelines/ui.md). dist/ always
// holds a tracked .gitkeep so the package compiles before any UI build.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler serves the built UI. A path that is not a file falls back to index.html.
// The security headers keep the page from being framed or loading foreign scripts.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err) // dist is embedded by the directive above; this cannot fail
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(root, name); err == nil && !info.IsDir() && name != "index.html" {
			if strings.HasPrefix(name, "assets/") {
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		// Served directly: FileServer would redirect a request for /index.html to /.
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "the UI is not built", http.StatusNotFound)
			return
		}
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

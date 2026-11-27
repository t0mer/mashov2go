package api

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

// The OpenAPI document and the endpoint list page are embedded, so the binary
// describes exactly the routes it serves (see spec_test.go).
//
//go:embed openapi.json docs/index.html docs/docs.js
var specFS embed.FS

func (s *Server) specRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/openapi.json", serveFile("openapi.json", "application/json; charset=utf-8", "public, max-age=300"))
	s.handle(mux, "GET /api/docs", serveFile("docs/index.html", "text/html; charset=utf-8", "no-cache"))
	s.handle(mux, "GET /api/docs.js", serveFile("docs/docs.js", "text/javascript; charset=utf-8", "no-cache"))
}

func serveFile(name, contentType, cache string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body, err := specFS.ReadFile(name)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	})
}

// specOperations returns "METHOD /path" for every operation in the document.
func specOperations() (map[string]bool, error) {
	raw, err := specFS.ReadFile("openapi.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	ops := map[string]bool{}
	for path, methods := range doc.Paths {
		for m := range methods {
			ops[strings.ToUpper(m)+" "+path] = true
		}
	}
	return ops, nil
}

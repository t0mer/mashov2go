package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The OpenAPI document must describe exactly the routes the server registers.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	srv, _ := newServer(t)
	ops, err := specOperations()
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, p := range srv.Patterns() {
		routes[p] = true
	}
	for r := range routes {
		if !ops[r] {
			t.Errorf("route %q is not described in openapi.json", r)
		}
	}
	for o := range ops {
		if !routes[o] {
			t.Errorf("openapi.json describes %q, which is not a route", o)
		}
	}
}

func TestSpecAndDocsAreServedPublicly(t *testing.T) {
	_, h := newServer(t)
	rec := call(h, http.MethodGet, "/api/openapi.json", nil, nil, nil)
	var doc map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &doc) != nil || doc["openapi"] != "3.1.0" {
		t.Fatalf("spec: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/docs", nil, nil, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/api/docs.js") {
		t.Fatalf("docs page: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/docs.js", nil, nil, nil); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") == "" {
		t.Fatalf("docs script: %d", rec.Code)
	}
}

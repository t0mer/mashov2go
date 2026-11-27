package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootServesIndexWithSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
		t.Fatalf("index: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("headers: %v", rec.Header())
	}
}

func TestUnknownPathFallsBackToIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/somewhere", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
		t.Fatalf("fallback: %d", rec.Code)
	}
}

func TestTraversalDoesNotLeaveDist(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/../../go.mod", nil))
	if strings.Contains(rec.Body.String(), "module ") {
		t.Fatal("served a file outside dist")
	}
}

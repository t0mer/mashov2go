package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/t0mer/mashov2go/internal/metrics"
)

func TestMetricsNeedReadScopeAndExposeCounters(t *testing.T) {
	srv, h := newServer(t)
	srv.Metrics = metrics.NewRegistry()
	srv.Metrics.Counter("mashov2go_plans_sent_total", "plans").Inc()
	cookie, _ := signIn(t, h)

	if rec := call(h, http.MethodGet, "/metrics", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous metrics: %d, want 401", rec.Code)
	}
	rec := call(h, http.MethodGet, "/metrics", nil, cookie, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mashov2go_plans_sent_total 1") {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
}

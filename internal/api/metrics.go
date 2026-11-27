package api

import "net/http"

// metricsHandler serves the counters in Prometheus text format. Scrapers can
// send a read-scope Bearer token.
func (s *Server) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_ = s.Metrics.Write(w)
}

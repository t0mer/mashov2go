package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// heartbeat keeps idle streams open through proxies and tunnels.
const heartbeat = 25 * time.Second

// eventStream streams new events as Server-Sent Events (CLAUDE.md §9). Each event
// is one "event:" line named by its category, with the event as JSON in "data:".
// Browsers reconnect by themselves; the session cookie authenticates the stream.
func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if s.Broker == nil {
		problem(w, http.StatusServiceUnavailable, "Stream unavailable", "the event broker is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem(w, http.StatusInternalServerError, "Internal error", "streaming is not supported")
		return
	}
	events, cancel := s.Broker.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-events:
			if !open {
				return
			}
			b, err := json.Marshal(eventView{Category: ev.Category, Kind: ev.Kind,
				OccurredAt: ev.OccurredAt.Format(time.RFC3339), DetectedAt: ev.DetectedAt.Format(time.RFC3339),
				Source: ev.Source, Data: ev.Data})
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Category, b); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

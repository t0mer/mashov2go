package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/t0mer/mashov2go/internal/store"
)

type deliveryView struct {
	ID        int64  `json:"id"`
	EventID   int64  `json:"eventId"`
	MemberID  int64  `json:"memberId"`
	Channel   string `json:"channel"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	NextAt    string `json:"nextAt"`
	LastError string `json:"lastError"`
	Body      string `json:"body"`
}

type eventView struct {
	ID         int64  `json:"id"`
	Category   string `json:"category"`
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurredAt"`
	DetectedAt string `json:"detectedAt"`
	Source     string `json:"source"`
	Data       string `json:"data"`
}

var deliveryStatuses = map[string]bool{
	"": true, "pending": true, "sent": true, "failed": true, "skipped_no_channel": true,
}

func (s *Server) activityRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/v1/deliveries", s.authed("read", http.HandlerFunc(s.listDeliveries)))
	s.handle(mux, "POST /api/v1/deliveries/{id}/retry", s.authed("admin", http.HandlerFunc(s.retryDelivery)))
	s.handle(mux, "GET /api/v1/events", s.authed("read", http.HandlerFunc(s.listEvents)))
}

// limitParam reads ?limit=, default 100 and at most 500.
func limitParam(r *http.Request) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 100, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 500 {
		return 0, false
	}
	return n, true
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if !deliveryStatuses[status] {
		problem(w, http.StatusBadRequest, "Bad request", "status must be pending, sent, failed or skipped_no_channel")
		return
	}
	limit, ok := limitParam(r)
	if !ok {
		problem(w, http.StatusBadRequest, "Bad request", "limit must be 1 to 500")
		return
	}
	list, err := s.Store.ListDeliveries(r.Context(), status, limit)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing deliveries failed")
		return
	}
	out := make([]deliveryView, 0, len(list))
	for _, d := range list {
		out = append(out, deliveryView{ID: d.ID, EventID: d.EventID, MemberID: d.MemberID, Channel: d.Channel,
			Status: d.Status, Attempts: d.Attempts, NextAt: d.NextAt.Format(time.RFC3339),
			LastError: d.LastError, Body: d.Body})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the id is not valid")
		return
	}
	err = s.Store.RetryDelivery(r.Context(), id, s.now())
	switch {
	case errors.Is(err, store.ErrNotFound):
		problem(w, http.StatusNotFound, "Not found", "no such delivery")
	case errors.Is(err, store.ErrNotRetriable):
		problem(w, http.StatusConflict, "Conflict", "only failed or skipped deliveries can be retried")
	case err != nil:
		problem(w, http.StatusInternalServerError, "Internal error", "retrying the delivery failed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// listEvents returns events detected since ?since= (RFC 3339, default 7 days
// ago), optionally filtered by ?category=.
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	since := s.now().Add(-7 * 24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", "since must be an RFC 3339 time")
			return
		}
		since = t
	}
	limit, ok := limitParam(r)
	if !ok {
		problem(w, http.StatusBadRequest, "Bad request", "limit must be 1 to 500")
		return
	}
	list, err := s.Store.ListEventsFiltered(r.Context(), r.URL.Query().Get("category"), since, limit)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing events failed")
		return
	}
	out := make([]eventView, 0, len(list))
	for _, e := range list {
		out = append(out, eventView{ID: e.ID, Category: e.Category, Kind: e.Kind,
			OccurredAt: e.OccurredAt.Format(time.RFC3339), DetectedAt: e.DetectedAt.Format(time.RFC3339),
			Source: e.Source, Data: e.Data})
	}
	writeJSON(w, http.StatusOK, out)
}

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

// seedFailedDelivery creates an event and a failed delivery for a new child.
func seedFailedDelivery(t *testing.T, srv *Server) (deliveryID int64) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	kid, err := srv.Store.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Noa", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972501234567", QuietStart: "21:00", QuietEnd: "07:00",
		PlanTime: "19:30", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store.InsertEvent(ctx, store.Event{Category: "homework", Kind: "new",
		OccurredAt: now, DetectedAt: now, Source: "diff", DedupeKey: "hw-1", Data: `{"subject":"Math"}`}); err != nil {
		t.Fatal(err)
	}
	eventID, _, err := srv.Store.EventIDByDedupe(ctx, "hw-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store.EnqueueDelivery(ctx, store.Delivery{EventID: eventID, MemberID: kid.ID, Channel: "whatsapp",
		NextAt: now, Body: "homework text"}); err != nil {
		t.Fatal(err)
	}
	list, err := srv.Store.ListDeliveries(ctx, "", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("seeded deliveries: %d %v", len(list), err)
	}
	if err := srv.Store.MarkDeliveryFailed(ctx, list[0].ID, 6, "provider down"); err != nil {
		t.Fatal(err)
	}
	return list[0].ID
}

func TestDeliveriesFilterRetryAndValidation(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	id := seedFailedDelivery(t, srv)

	rec := call(h, http.MethodGet, "/api/v1/deliveries?status=failed", nil, cookie, nil)
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), `"lastError":"provider down"`) {
		t.Fatalf("failed list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/deliveries?status=sent", nil, cookie, nil); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("sent list should be empty: %s", rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/deliveries?status=lost", nil, cookie, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/deliveries?limit=9999", nil, cookie, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", rec.Code)
	}

	path := "/api/v1/deliveries/" + itoa(id) + "/retry"
	if rec := adminCall(h, cookie, csrf, http.MethodPost, path, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, path, nil); rec.Code != http.StatusConflict {
		t.Fatalf("retry a pending delivery: %d, want 409", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/deliveries/999/retry", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("retry missing: %d, want 404", rec.Code)
	}
}

func TestEventsFilterByCategoryAndSince(t *testing.T) {
	srv, h := newServer(t)
	cookie, _ := signIn(t, h)
	seedFailedDelivery(t, srv)

	if rec := call(h, http.MethodGet, "/api/v1/events?category=homework", nil, cookie, nil); rec.Code != http.StatusOK ||
		!contains(rec.Body.String(), `"category":"homework"`) {
		t.Fatalf("events: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/events?category=grades", nil, cookie, nil); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("grades should be empty: %s", rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/events?since=yesterday", nil, cookie, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad since: %d", rec.Code)
	}
}

func TestActivityScopes(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	id := seedFailedDelivery(t, srv)
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "dash", "scope": "read"}).Body.String())["token"].(string)
	bearer := map[string]string{"Authorization": "Bearer " + tok}

	if rec := call(h, http.MethodGet, "/api/v1/deliveries", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("read token listing deliveries: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/deliveries/"+itoa(id)+"/retry", nil, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token retrying: %d, want 403", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/events", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous events: %d", rec.Code)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

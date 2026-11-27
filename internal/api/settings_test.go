package api

import (
	"net/http"
	"testing"
	"time"
)

func TestGeneralSettingsShowDefaultsAsReadableDurations(t *testing.T) {
	_, h := newServer(t)
	cookie, _ := signIn(t, h)
	rec := call(h, http.MethodGet, "/api/v1/settings/general", nil, cookie, nil)
	got := decodeMap(t, rec.Body.String())
	if rec.Code != http.StatusOK || got["timezone"] != "Asia/Jerusalem" || got["syncFastInterval"] != "5m0s" ||
		got["eventsRetention"] != "8760h0m0s" || got["preserveUnread"] != true {
		t.Fatalf("defaults: %d %v", rec.Code, got)
	}
}

func TestPatchGeneralAppliesAndNotifies(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	sub := srv.Settings.Subscribe()

	rec := adminCall(h, cookie, csrf, http.MethodPatch, "/api/v1/settings/general",
		map[string]any{"syncFastInterval": "2m", "planDefaultTime": "20:00", "shabbatMode": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec.Body.String())
	if got["syncFastInterval"] != "2m0s" || got["planDefaultTime"] != "20:00" || got["shabbatMode"] != true {
		t.Fatalf("patched: %v", got)
	}
	select {
	case changed := <-sub:
		if changed.SyncFastInterval != 2*time.Minute {
			t.Fatalf("subscriber saw %v", changed.SyncFastInterval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber not notified")
	}
}

func TestPatchGeneralRejectsInvalidWithoutChanging(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	cases := map[string]map[string]any{
		"unknown timezone":  {"timezone": "Mars/Olympus"},
		"gap below minimum": {"mashovMinGap": "100ms"},
		"bad duration":      {"syncFullInterval": "soon"},
		"bad active hours":  {"activeHours": "7-9"},
		"bad profile":       {"mashovProfile": "iphone"},
		"retention too low": {"eventsRetention": "1h"},
		"unknown field":     {"notAField": 1},
	}
	for name, body := range cases {
		rec := adminCall(h, cookie, csrf, http.MethodPatch, "/api/v1/settings/general", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
	if srv.Settings.Get().Timezone != "Asia/Jerusalem" || srv.Settings.Get().MashovMinGap != 750*time.Millisecond {
		t.Fatal("rejected change was applied")
	}
}

func TestGeneralSettingsReadScopes(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "dash", "scope": "read"}).Body.String())["token"].(string)
	bearer := map[string]string{"Authorization": "Bearer " + tok}

	if rec := call(h, http.MethodGet, "/api/v1/settings/general", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("read token reading settings: %d", rec.Code)
	}
	if rec := call(h, http.MethodPatch, "/api/v1/settings/general", map[string]any{"shabbatMode": true}, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token changing settings: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/settings/general", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous settings: %d", rec.Code)
	}
}

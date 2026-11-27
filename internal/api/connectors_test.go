package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const greenToken = "abcdefghijklmnopqrstu"

// fakeGreenAPI answers getStateInstance with "authorized" and accepts sendMessage.
type fakeGreenAPI struct {
	mu    sync.Mutex
	sends []string
}

func (f *fakeGreenAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/getStateInstance/"+greenToken):
		_, _ = io.WriteString(w, `{"stateInstance":"authorized"}`)
	case strings.HasSuffix(r.URL.Path, "/sendMessage/"+greenToken):
		b, _ := io.ReadAll(r.Body)
		f.sends = append(f.sends, string(b))
		_, _ = io.WriteString(w, `{"idMessage":"m1"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func greenConfig(url string) map[string]any {
	return map[string]any{"api_url": url, "instance_id": "1101", "token": greenToken}
}

func createConnector(t *testing.T, h http.Handler, cookie *http.Cookie, csrf string, body map[string]any) map[string]any {
	t.Helper()
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create connector: %d %s", rec.Code, rec.Body.String())
	}
	return decodeMap(t, rec.Body.String())
}

func TestConnectorConfigIsValidatedAndNeverShown(t *testing.T) {
	fake := &fakeGreenAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)

	created := createConnector(t, h, cookie, csrf, map[string]any{"type": "greenapi", "name": "home", "config": greenConfig(srv.URL)})
	if created["type"] != "greenapi" || created["enabled"] != true {
		t.Fatalf("created: %v", created)
	}
	list := call(h, http.MethodGet, "/api/v1/connectors", nil, cookie, nil)
	if strings.Contains(list.Body.String(), greenToken) || strings.Contains(list.Body.String(), "instance_id") {
		t.Fatalf("config leaked in listing: %s", list.Body.String())
	}

	bad := map[string]any{"type": "greenapi", "name": "x", "config": map[string]any{"api_url": srv.URL, "instance_id": "11a", "token": greenToken}}
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors", bad)
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), greenToken) {
		t.Fatalf("bad config: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors",
		map[string]any{"type": "smtp", "name": "x", "config": greenConfig(srv.URL)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type: %d", rec.Code)
	}
}

func TestTelegramCannotBeDefaultWhatsApp(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors", map[string]any{
		"type": "telegram", "name": "bot", "isDefault": true,
		"config": map[string]any{"bot_token": "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("telegram default: %d, want 400", rec.Code)
	}
}

func TestSetDefaultHealthAndTestSend(t *testing.T) {
	fake := &fakeGreenAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	conn := createConnector(t, h, cookie, csrf, map[string]any{"type": "greenapi", "name": "home", "config": greenConfig(srv.URL)})
	id := itoa(int64(conn["id"].(float64)))

	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors/"+id+"/default", nil); rec.Code != http.StatusOK ||
		decodeMap(t, rec.Body.String())["isDefault"] != true {
		t.Fatalf("set default: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/connectors/"+id+"/health", nil, cookie, nil); rec.Code != http.StatusOK ||
		decodeMap(t, rec.Body.String())["status"] != "ok" {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}

	kid := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234567"}).Body.String())
	memberID := kid["id"].(float64)
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors/"+id+"/test", map[string]any{"memberId": memberID})
	if rec.Code != http.StatusOK || len(fake.sends) != 1 || !strings.Contains(fake.sends[0], "972501234567@c.us") {
		t.Fatalf("test send: %d %s sends=%v", rec.Code, rec.Body.String(), fake.sends)
	}
}

func TestDeleteBlockedWhileInUse(t *testing.T) {
	fake := &fakeGreenAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	conn := createConnector(t, h, cookie, csrf, map[string]any{"type": "greenapi", "name": "home", "config": greenConfig(srv.URL)})
	id := itoa(int64(conn["id"].(float64)))

	kid := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]any{"name": "Noa", "whatsappPhone": "0501234567", "whatsappConnectorId": conn["id"]}).Body.String())
	if kid["whatsappConnectorId"] == nil {
		t.Fatalf("member not linked to connector: %v", kid)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodDelete, "/api/v1/connectors/"+id, nil); rec.Code != http.StatusConflict {
		t.Fatalf("delete in use: %d, want 409", rec.Code)
	}
}

func TestUpdateNameAndEnabledKeepsConfig(t *testing.T) {
	fake := &fakeGreenAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	conn := createConnector(t, h, cookie, csrf, map[string]any{"type": "greenapi", "name": "home", "config": greenConfig(srv.URL)})
	id := itoa(int64(conn["id"].(float64)))

	rec := adminCall(h, cookie, csrf, http.MethodPatch, "/api/v1/connectors/"+id, map[string]any{"name": "garage", "enabled": false})
	got := decodeMap(t, rec.Body.String())
	if rec.Code != http.StatusOK || got["name"] != "garage" || got["enabled"] != false {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/connectors/"+id+"/health", nil, cookie, nil); rec.Code != http.StatusOK ||
		decodeMap(t, rec.Body.String())["status"] != "ok" {
		t.Fatal("config lost by a name change")
	}
}

func TestConnectorRoutesRespectScopes(t *testing.T) {
	fake := &fakeGreenAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "dash", "scope": "read"}).Body.String())["token"].(string)
	bearer := map[string]string{"Authorization": "Bearer " + tok}

	if rec := call(h, http.MethodGet, "/api/v1/connectors", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("read token listing: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/connectors", map[string]any{"type": "greenapi", "name": "x", "config": greenConfig(srv.URL)}, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token creating connector: %d", rec.Code)
	}
}

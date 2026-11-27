package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t0mer/mashov2go/internal/store"
)

// adminCall performs a request as the signed-in admin, adding the CSRF token.
func adminCall(h http.Handler, cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
	return call(h, method, path, body, cookie, map[string]string{"X-CSRF-Token": csrf})
}

func decodeMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return m
}

func TestCreateChildAppliesDefaultsAndNormalisesPhone(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)

	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "  Noa ", "whatsappPhone": "050-123-4567"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec.Body.String())
	if m["name"] != "Noa" || m["whatsappPhone"] != "972501234567" || m["role"] != "child" {
		t.Fatalf("fields: %v", m)
	}
	if m["planTime"] != "19:30" || m["quietStart"] != "21:00" || m["quietEnd"] != "07:00" || m["channel"] != "whatsapp" {
		t.Fatalf("child defaults: %v", m)
	}
}

func TestParentDefaultsDifferFromChild(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/parents",
		map[string]string{"name": "Tomer", "whatsappPhone": "0501234567"})
	m := decodeMap(t, rec.Body.String())
	if rec.Code != http.StatusCreated || m["quietStart"] != "22:30" || m["quietEnd"] != "06:30" {
		t.Fatalf("parent: %d %v", rec.Code, m)
	}
}

func TestInvalidMembersAreRejected(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	cases := map[string]map[string]any{
		"no name":            {"whatsappPhone": "0501234567"},
		"no phone":           {"name": "Noa"},
		"bad phone":          {"name": "Noa", "whatsappPhone": "12"},
		"bad plan time":      {"name": "Noa", "whatsappPhone": "0501234567", "planTime": "25:00"},
		"telegram not bound": {"name": "Noa", "channel": "telegram"},
		"unknown connector":  {"name": "Noa", "whatsappPhone": "0501234567", "whatsappConnectorId": 999},
		"unknown field":      {"name": "Noa", "whatsappPhone": "0501234567", "role": "parent"},
	}
	for name, body := range cases {
		if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
}

func TestRoleRoutesDoNotCrossOver(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234567"})
	id := decodeMap(t, rec.Body.String())["id"].(float64)
	path := "/api/v1/parents/" + itoa(int64(id))

	if got := adminCall(h, cookie, csrf, http.MethodGet, path, nil); got.Code != http.StatusNotFound {
		t.Fatalf("child read through parents: %d", got.Code)
	}
	if got := adminCall(h, cookie, csrf, http.MethodPatch, path, map[string]any{"name": "x"}); got.Code != http.StatusNotFound {
		t.Fatalf("child patch through parents: %d", got.Code)
	}
	list := call(h, http.MethodGet, "/api/v1/parents", nil, cookie, nil)
	if strings.Contains(list.Body.String(), "Noa") {
		t.Fatal("child listed as a parent")
	}
}

func TestPatchAndDeleteChild(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234567"})
	id := itoa(int64(decodeMap(t, rec.Body.String())["id"].(float64)))
	path := "/api/v1/children/" + id

	if got := adminCall(h, cookie, csrf, http.MethodPatch, path, map[string]any{"planTime": "20:00", "active": false}); got.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", got.Code, got.Body.String())
	}
	got := decodeMap(t, call(h, http.MethodGet, path, nil, cookie, nil).Body.String())
	if got["planTime"] != "20:00" || got["active"] != false || got["name"] != "Noa" {
		t.Fatalf("after patch: %v", got)
	}
	if bad := adminCall(h, cookie, csrf, http.MethodPatch, path, map[string]any{"planTime": "99:99"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad patch: %d", bad.Code)
	}
	if del := adminCall(h, cookie, csrf, http.MethodDelete, path, nil); del.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", del.Code)
	}
	if g := call(h, http.MethodGet, path, nil, cookie, nil); g.Code != http.StatusNotFound {
		t.Fatalf("deleted child still readable: %d", g.Code)
	}
}

func TestLinkChildToMashovStudentOnce(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	acc, err := srv.Store.CreateAccount(t.Context(), store.Account{Name: "home", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	noa := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234567"}).Body.String())
	dana := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Dana", "whatsappPhone": "0501234568"}).Body.String())
	link := map[string]any{"accountId": acc.ID, "childGuid": "guid-1"}

	nid := itoa(int64(noa["id"].(float64)))
	did := itoa(int64(dana["id"].(float64)))
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+nid+"/link", link); rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+nid+"/link", link); rec.Code != http.StatusConflict {
		t.Fatalf("second link of same child: %d, want 409", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+did+"/link", link); rec.Code != http.StatusConflict {
		t.Fatalf("same student for another child: %d, want 409", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+did+"/link",
		map[string]any{"accountId": 999, "childGuid": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown account: %d, want 400", rec.Code)
	}
	got := decodeMap(t, call(h, http.MethodGet, "/api/v1/children/"+nid, nil, cookie, nil).Body.String())
	if link, ok := got["link"].(map[string]any); !ok || link["childGuid"] != "guid-1" {
		t.Fatalf("link not shown: %v", got)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodDelete, "/api/v1/children/"+nid+"/link", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d", rec.Code)
	}
}

func TestRecipientsOverrideAndReset(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	kid := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234567"}).Body.String())
	path := "/api/v1/children/" + itoa(int64(kid["id"].(float64))) + "/recipients"

	defaults := decodeMap(t, call(h, http.MethodGet, path, nil, cookie, nil).Body.String())
	if defaults["homework"] != "child" || defaults["grades"] != "parents" {
		t.Fatalf("defaults: %v", defaults)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, map[string]string{"grades": "both"}); rec.Code != http.StatusOK ||
		decodeMap(t, rec.Body.String())["grades"] != "both" {
		t.Fatalf("override: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, map[string]string{"nope": "both"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown category: %d", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, map[string]string{"grades": "everyone"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown recipient: %d", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, path+"/reset", nil); rec.Code != http.StatusOK ||
		decodeMap(t, rec.Body.String())["grades"] != "parents" {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
}

func TestReadTokenCannotWriteFamily(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "dash", "scope": "read"}).Body.String())["token"].(string)
	bearer := map[string]string{"Authorization": "Bearer " + tok}

	if rec := call(h, http.MethodGet, "/api/v1/children", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("read token listing children: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/children", map[string]string{"name": "x", "whatsappPhone": "0501234567"}, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token creating child: %d, want 403", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/children", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous listing: %d, want 401", rec.Code)
	}
}

func TestParentMutesReplaceAndValidate(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	par := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/parents",
		map[string]string{"name": "Tomer", "whatsappPhone": "0501234567"}).Body.String())
	kid := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa", "whatsappPhone": "0501234568"}).Body.String())
	path := "/api/v1/parents/" + itoa(int64(par["id"].(float64))) + "/mutes"
	kidID := int64(kid["id"].(float64))

	body := map[string]any{"mutes": []map[string]any{{"childId": kidID, "category": "behave"}}}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, body); rec.Code != http.StatusOK || !contains(rec.Body.String(), `"category":"behave"`) {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, map[string]any{"mutes": []map[string]any{{"childId": kidID, "category": "weather"}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown category: %d", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, map[string]any{"mutes": []map[string]any{{"childId": 9999, "category": "behave"}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown child: %d", rec.Code)
	}
	parentAsChild := map[string]any{"mutes": []map[string]any{{"childId": int64(par["id"].(float64)), "category": "behave"}}}
	if rec := adminCall(h, cookie, csrf, http.MethodPut, path, parentAsChild); rec.Code != http.StatusBadRequest {
		t.Fatalf("parent used as child: %d", rec.Code)
	}
	// A failed request leaves the previous mutes in place.
	if got := call(h, http.MethodGet, path, nil, cookie, nil); !contains(got.Body.String(), `"category":"behave"`) {
		t.Fatalf("mutes changed by a rejected request: %s", got.Body.String())
	}
}

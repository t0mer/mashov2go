package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestTemplatesDefaultsOverrideResetAndPreview(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)

	list := call(h, http.MethodGet, "/api/v1/templates", nil, cookie, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"category":"grades"`) ||
		!strings.Contains(list.Body.String(), `"isDefault":true`) {
		t.Fatalf("defaults: %d %s", list.Code, list.Body.String())
	}

	put := adminCall(h, cookie, csrf, http.MethodPut, "/api/v1/templates",
		map[string]any{"category": "grades", "language": "he", "audience": "parent", "body": "ציון {{.subject}}: {{.value}}"})
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"isDefault":false`) {
		t.Fatalf("put: %d %s", put.Code, put.Body.String())
	}
	list = call(h, http.MethodGet, "/api/v1/templates", nil, cookie, nil)
	if !strings.Contains(list.Body.String(), "ציון {{.subject}}: {{.value}}") {
		t.Fatalf("override not listed: %s", list.Body.String())
	}

	if del := adminCall(h, cookie, csrf, http.MethodDelete, "/api/v1/templates/grades/he/parent", nil); del.Code != http.StatusNoContent {
		t.Fatalf("reset: %d", del.Code)
	}
	list = call(h, http.MethodGet, "/api/v1/templates", nil, cookie, nil)
	if strings.Contains(list.Body.String(), "ציון {{.subject}}") {
		t.Fatal("reset did not restore the default")
	}

	prev := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/templates/preview",
		map[string]any{"category": "homework", "body": "{{.subject}}: {{.text}}"})
	if prev.Code != http.StatusOK || !strings.Contains(prev.Body.String(), "Math: pages 40-42") {
		t.Fatalf("preview: %d %s", prev.Code, prev.Body.String())
	}
}

func TestTemplatesRejectBadInput(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	cases := map[string]map[string]any{
		"syntax error":        {"category": "grades", "language": "he", "audience": "parent", "body": "{{.value"},
		"missing field":       {"category": "grades", "language": "he", "audience": "parent", "body": "{{.nope}}"},
		"unknown category":    {"category": "weather", "language": "he", "audience": "parent", "body": "x"},
		"unknown language":    {"category": "grades", "language": "fr", "audience": "parent", "body": "x"},
		"no such combination": {"category": "homework", "language": "he", "audience": "parent", "body": "x"},
		"empty body":          {"category": "grades", "language": "he", "audience": "parent", "body": "  "},
	}
	for name, body := range cases {
		if rec := adminCall(h, cookie, csrf, http.MethodPut, "/api/v1/templates", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/templates/preview",
		map[string]any{"category": "grades", "body": "{{.value"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("preview of a broken body: %d", rec.Code)
	}
}

func TestTemplatesReadTokenCannotChange(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "dash", "scope": "read"}).Body.String())["token"].(string)
	bearer := map[string]string{"Authorization": "Bearer " + tok}
	if rec := call(h, http.MethodGet, "/api/v1/templates", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("read token listing templates: %d", rec.Code)
	}
	if rec := call(h, http.MethodPut, "/api/v1/templates", map[string]any{"category": "grades", "language": "he", "audience": "parent", "body": "x"}, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token saving a template: %d", rec.Code)
	}
}

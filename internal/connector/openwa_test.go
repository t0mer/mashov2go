package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testKey = "owa_k1_" + strings.Repeat("a1", 32)

func openwaConfig(baseURL string) OpenWAConfig {
	return OpenWAConfig{BaseURL: baseURL, InstanceID: "sess-1", APIKey: testKey}
}

func TestNewOpenWAValidation(t *testing.T) {
	good, err := NewOpenWA(OpenWAConfig{BaseURL: " https://wa.example.com/api/docs ", InstanceID: " sess-1 ", APIKey: " " + testKey + " "}, nil)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if good.base.String() != "https://wa.example.com" || good.session != "sess-1" {
		t.Fatalf("normalised base=%q session=%q", good.base.String(), good.session)
	}
	cases := map[string]OpenWAConfig{
		"plain http":    {BaseURL: "http://wa.example.com", InstanceID: "s", APIKey: testKey},
		"bad key":       {BaseURL: "https://wa.example.com", InstanceID: "s", APIKey: "owa_k1_xyz"},
		"empty session": {BaseURL: "https://wa.example.com", InstanceID: "", APIKey: testKey},
		"no host":       {BaseURL: "https://", InstanceID: "s", APIKey: testKey},
	}
	for name, cfg := range cases {
		if _, err := NewOpenWA(cfg, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOpenWASendTextRequestShape(t *testing.T) {
	var gotPath, gotKey, gotCT string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotCT = r.Method+" "+r.URL.Path, r.Header.Get("X-API-Key"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1"}`)
	}))
	defer srv.Close()
	o, err := NewOpenWA(openwaConfig(srv.URL), srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	if err := o.SendTo(context.Background(), "+972 50-123-4567", Message{Text: "שלום"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "POST /api/sessions/sess-1/messages/send-text" || gotKey != testKey || gotCT != "application/json" {
		t.Fatalf("request: %q key-set=%v ct=%q", gotPath, gotKey == testKey, gotCT)
	}
	if gotBody["chatId"] != "972501234567@c.us" || gotBody["text"] != "שלום" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestOpenWAKeepsExistingChatIDs(t *testing.T) {
	var chats []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		chats = append(chats, b["chatId"])
	}))
	defer srv.Close()
	o, _ := NewOpenWA(openwaConfig(srv.URL), srv.Client())
	for _, addr := range []string{"120363000000000001@g.us", "5551234@lid"} {
		if err := o.SendTo(context.Background(), addr, Message{Text: "x"}); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	if len(chats) != 2 || chats[0] != "120363000000000001@g.us" || chats[1] != "5551234@lid" {
		t.Fatalf("chat ids = %v", chats)
	}
	if err := o.SendTo(context.Background(), "0501234567", Message{Text: "x"}); !IsPermanent(err) {
		t.Fatalf("local-format phone: err=%v, want permanent", err)
	}
}

func TestOpenWASendErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		header    string
		body      string
		permanent bool
		retry     time.Duration
	}{
		{"bad request", 400, "", `{"message":"bad"}`, true, 0},
		{"unauthorised", 401, "", `{"message":"no"}`, true, 0},
		{"session missing", 404, "", "", true, 0},
		{"too large", 413, "", "", true, 0},
		{"server error", 500, "", "", false, 0},
		{"unavailable", 503, "", "", false, 0},
		{"conflict", 409, "", "", false, 0},
		{"pacing cap", 429, "", `{"code":"SEND_PACING_LIMITED","retryAfterSeconds":120}`, false, 120 * time.Second},
		{"rate limit header", 429, "30", `{}`, false, 30 * time.Second},
		{"pacing cap capped", 429, "", `{"code":"SEND_PACING_LIMITED","retryAfterSeconds":999999}`, false, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer srv.Close()
			o, _ := NewOpenWA(openwaConfig(srv.URL), srv.Client())
			err := o.SendTo(context.Background(), "972501234567", Message{Text: "x"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if IsPermanent(err) != c.permanent {
				t.Fatalf("permanent = %v, want %v (%v)", IsPermanent(err), c.permanent, err)
			}
			if RetryAfterOf(err) != c.retry {
				t.Fatalf("retry after = %v, want %v", RetryAfterOf(err), c.retry)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatal("api key leaked into error")
			}
		})
	}
}

func TestOpenWAHealth(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   Status
	}{
		"ready":      {200, `{"status":"ready"}`, StatusOK},
		"not ready":  {200, `{"status":"qr"}`, StatusDown},
		"missing":    {404, `{}`, StatusDown},
		"bad json":   {200, `<html>`, StatusDown},
		"server 500": {500, ``, StatusDown},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/sessions/sess-1" {
					t.Errorf("health request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer srv.Close()
			o, _ := NewOpenWA(openwaConfig(srv.URL), srv.Client())
			if got := o.Health(context.Background()); got != c.want {
				t.Fatalf("health = %s, want %s", got, c.want)
			}
		})
	}
}

package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
)

func TestEventStreamDeliversPublishedEvents(t *testing.T) {
	srv, h := newServer(t)
	srv.Broker = syncer.NewBroker()
	cookie, _ := signIn(t, h)
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/events/stream", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// The subscription starts when the handler runs, so publish until a line arrives.
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: homework") {
				found <- sc.Text()
				return
			}
		}
	}()
	deadline := time.After(5 * time.Second)
	for {
		srv.Broker.Publish(store.Event{Category: "homework", Kind: "homework.new", Source: "diff", Data: `{"subject":"Math"}`,
			OccurredAt: time.Now(), DetectedAt: time.Now()})
		select {
		case <-found:
			return
		case <-deadline:
			t.Fatal("no event arrived on the stream")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestEventStreamNeedsAuthAndBroker(t *testing.T) {
	srv, h := newServer(t)
	if rec := call(h, http.MethodGet, "/api/v1/events/stream", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous stream: %d", rec.Code)
	}
	cookie, _ := signIn(t, h)
	if rec := call(h, http.MethodGet, "/api/v1/events/stream", nil, cookie, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no broker: %d, want 503", rec.Code)
	}
	_ = srv
}

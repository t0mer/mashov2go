package mashov

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotificationsDecodeIncludingDecimalTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/notifications" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `[{"id":9001,"eventType":111.0,"eventText":"ציון חדש","eventTime":"2026-11-27T10:00:00",`+
			`"eventUserId":"sg-1","eventKey":"55","eventData":"{}","readTime":null}]`)
	}))
	defer srv.Close()
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", MinGap: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	list, err := c.Notifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID.Int() != 9001 || list[0].EventType.Int() != 111 || list[0].EventUserID != "sg-1" {
		t.Fatalf("decoded = %+v", list)
	}
}

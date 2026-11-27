package mashov

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBehaveDecodesSchoolWording(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/students/sg-1/behave" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `[{"achvaCode":21,"achvaName":"איחור","achvaAval":-1,"eventCode":21,`+
			`"justification":"","justified":-1,"lesson":3,"lessonDate":"2026-11-02T00:00:00","lessonId":77,`+
			`"subject":"Math","studentGuid":"sg-1","timestamp":"2026-11-02T10:00:00"}]`)
	}))
	defer srv.Close()
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", MinGap: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.Behave(context.Background(), "sg-1", time.Now().AddDate(0, -1, 0), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AchvaName != "איחור" || items[0].Lesson != 3 || items[0].LessonID != 77 {
		t.Fatalf("decoded = %+v", items)
	}
}

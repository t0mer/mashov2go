package mashov

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGradesWindowAndDecoding(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/students/sg-1/grades" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":501,"gradingEventId":77,"gradingEvent":"Quiz","gradeType":"grade",`+
			`"gradeTypeId":1,"gradingPeriod":1,"gradeRate":0,"rate":92.0,"rangeGrade":"","subjectName":"Math",`+
			`"groupId":10,"groupName":"7-2","teacherName":"T","eventDate":"2026-11-02T00:00:00",`+
			`"timestamp":"2026-11-03T08:00:00","studentGuid":"sg-1","year":2027}]`)
	}))
	defer srv.Close()
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", MinGap: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	grades, err := c.Grades(context.Background(), "sg-1", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(grades) != 1 || grades[0].ID != 501 || grades[0].Rate != 92 || grades[0].SubjectName != "Math" {
		t.Fatalf("decoded = %+v", grades)
	}
	if !strings.Contains(gotQuery, "start=2026-09-01T00%3A00%3A00Z") || !strings.Contains(gotQuery, "end=2026-11-27T00%3A00%3A00Z") {
		t.Fatalf("query = %q", gotQuery)
	}
}

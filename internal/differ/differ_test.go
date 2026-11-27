package differ

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/store"
)

func raws(t *testing.T, items ...string) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, len(items))
	for _, s := range items {
		out = append(out, json.RawMessage(s))
	}
	return out
}

func stored(cur []Current) []store.Item {
	now := time.Now()
	var out []store.Item
	for _, c := range cur {
		out = append(out, store.Item{Key: c.Key, ContentHash: c.Hash, Payload: c.Payload, FirstSeen: now, LastSeen: now})
	}
	return out
}

func TestKeyRules(t *testing.T) {
	cases := []struct {
		domain, raw, want string
	}{
		{"homework", `{"lessonId":7,"homework":"x"}`, "lesson:7"},
		{"grades", `{"id":7,"gradingEventId":99}`, "grade:7"},
		{"grades", `{"gradingEventId":99}`, "99"},
		{"behave", `{"lessonId":3,"eventCode":5,"lessonDate":"2026-11-02T00:00:00"}`, "3|5|2026-11-02T00:00:00"},
		{"maakav", `{"maakavId":12}`, "12"},
		{"timetable-changes", `{"groupId":1,"lessonDate":"2026-11-30T00:00:00","lesson":2,"action":2}`, "1|2026-11-30T00:00:00|2|2"},
		{"messageboard", `{"eventid":8}`, "8"},
	}
	for _, c := range cases {
		var obj map[string]any
		_ = json.Unmarshal([]byte(c.raw), &obj)
		got, err := KeyFor(c.domain, obj)
		if err != nil || got != c.want {
			t.Errorf("KeyFor(%s) = %q, %v; want %q", c.domain, got, err, c.want)
		}
	}
}

func TestHomeworkWithoutLessonIDFallsBackToHash(t *testing.T) {
	var obj map[string]any
	_ = json.Unmarshal([]byte(`{"subjectName":"Math","lessonDate":"2026-11-01","homework":"p1"}`), &obj)
	k, err := KeyFor("homework", obj)
	if err != nil || len(k) < 6 || k[:5] != "hash:" {
		t.Fatalf("key = %q, %v", k, err)
	}
}

func TestNormalizeRejectsDuplicateKeys(t *testing.T) {
	_, err := Normalize("grades", raws(t, `{"gradingEventId":1,"grade":"90"}`, `{"gradingEventId":1,"grade":"91"}`))
	if err == nil {
		t.Fatal("duplicate keys must be rejected")
	}
}

func TestBaselineReportsNothing(t *testing.T) {
	cur, _ := Normalize("maakav", raws(t, `{"maakavId":1}`, `{"maakavId":2}`))
	res := Classify(nil, cur, true)
	if len(res.Changes) != 0 || len(res.Removed) != 0 {
		t.Fatalf("baseline must be silent: %+v", res)
	}
}

func TestNewUpdatedRemovedAfterBaseline(t *testing.T) {
	prevCur, _ := Normalize("grades", raws(t,
		`{"gradingEventId":1,"grade":"90"}`,
		`{"gradingEventId":2,"grade":"70"}`))
	prev := stored(prevCur)

	cur, _ := Normalize("grades", raws(t,
		`{"gradingEventId":1,"grade":"95"}`, // updated
		`{"gradingEventId":3,"grade":"80"}`, // new; id 2 removed
	))
	res := Classify(prev, cur, false)

	if len(res.Changes) != 2 {
		t.Fatalf("changes = %+v", res.Changes)
	}
	kinds := map[string]string{}
	for _, c := range res.Changes {
		kinds[c.Current.Key] = c.Kind
	}
	if kinds["1"] != "updated" || kinds["3"] != "new" {
		t.Fatalf("kinds = %v", kinds)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "2" {
		t.Fatalf("removed = %v", res.Removed)
	}
}

func TestUnchangedItemProducesNoEvent(t *testing.T) {
	cur, _ := Normalize("maakav", raws(t, `{"maakavId":5,"remark":"x"}`))
	// Same content in a different key order must hash the same.
	again, _ := Normalize("maakav", raws(t, `{"remark":"x","maakavId":5}`))
	res := Classify(stored(cur), again, false)
	if len(res.Changes) != 0 {
		t.Fatalf("reordered JSON produced a change: %+v", res.Changes)
	}
}

// Two grades in one grading event are two items, not one.
func TestGradesKeyedByRecordID(t *testing.T) {
	cur, err := Normalize("grades", raws(t,
		`{"id":1,"gradingEventId":50,"rate":80}`,
		`{"id":2,"gradingEventId":50,"rate":90}`))
	if err != nil {
		t.Fatalf("two grades in one event rejected: %v", err)
	}
	if len(cur) != 2 || cur[0].Key == cur[1].Key {
		t.Fatalf("keys = %+v", cur)
	}
}

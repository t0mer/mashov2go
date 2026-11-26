package syncer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/store"
)

func newStore(t *testing.T) (*store.Store, int64) {
	t.Helper()
	key := make([]byte, 32)
	sealer, _ := crypto.NewSealer(key)
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "s.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	acc, err := st.CreateAccount(context.Background(), store.Account{Name: "a", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return st, acc.ID
}

func raws(s ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(s))
	for i, v := range s {
		out[i] = json.RawMessage(v)
	}
	return out
}

func TestBaselineIsSilentThenChangesEmitOnce(t *testing.T) {
	st, acc := newStore(t)
	broker := NewBroker()
	ch, cancel := broker.Subscribe()
	defer cancel()
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	sy := &Syncer{Store: st, Broker: broker, Now: func() time.Time { return now }}
	ctx := context.Background()

	// First sync: existing homework is stored, nothing is announced.
	r1, err := sy.Apply(ctx, acc, "c1", "homework", raws(`{"lessonId":1,"homework":"p1","subjectName":"Math","lessonDate":"2026-11-25T00:00:00"}`))
	if err != nil || !r1.Baseline || len(r1.Events) != 0 {
		t.Fatalf("baseline result = %+v, err %v", r1, err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("baseline published an event: %+v", ev)
	default:
	}

	// Second sync: one new item and one edited item.
	r2, err := sy.Apply(ctx, acc, "c1", "homework", raws(
		`{"lessonId":1,"homework":"p1 changed","subjectName":"Math","lessonDate":"2026-11-25T00:00:00"}`,
		`{"lessonId":2,"homework":"read","subjectName":"English","lessonDate":"2026-11-26T00:00:00"}`,
	))
	if err != nil || r2.Baseline || r2.New != 1 || r2.Updated != 1 {
		t.Fatalf("second result = %+v, err %v", r2, err)
	}
	select {
	case ev := <-ch:
		if ev.Category != "homework" {
			t.Fatalf("event category = %q", ev.Category)
		}
	case <-time.After(time.Second):
		t.Fatal("no event published to broker")
	}

	// Third sync with identical data: no new events.
	r3, err := sy.Apply(ctx, acc, "c1", "homework", raws(
		`{"lessonId":1,"homework":"p1 changed","subjectName":"Math","lessonDate":"2026-11-25T00:00:00"}`,
		`{"lessonId":2,"homework":"read","subjectName":"English","lessonDate":"2026-11-26T00:00:00"}`,
	))
	if err != nil || r3.New+r3.Updated != 0 {
		t.Fatalf("unchanged sync produced events: %+v", r3)
	}
}

func TestRemovalIsSoftDeletedWithoutEvent(t *testing.T) {
	st, acc := newStore(t)
	sy := &Syncer{Store: st}
	ctx := context.Background()
	if _, err := sy.Apply(ctx, acc, "c1", "maakav", raws(`{"maakavId":1,"remark":"a"}`)); err != nil {
		t.Fatal(err)
	}
	res, err := sy.Apply(ctx, acc, "c1", "maakav", raws())
	if err != nil || res.Removed != 1 || len(res.Events) != 0 {
		t.Fatalf("removal result = %+v, err %v", res, err)
	}
	items, _ := st.ListItems(ctx, acc, "c1", "maakav")
	if len(items) != 0 {
		t.Fatal("removed item still live")
	}
}

func TestUnknownDomainIsRejected(t *testing.T) {
	st, acc := newStore(t)
	sy := &Syncer{Store: st}
	if _, err := sy.Apply(context.Background(), acc, "c1", "bagrut", raws(`{}`)); err == nil {
		t.Fatal("domain without a category must be rejected")
	}
}

func TestJitterStaysWithinBounds(t *testing.T) {
	base := 60 * time.Minute
	lo := Jitter(base, 0.15, func() float64 { return 0 })
	hi := Jitter(base, 0.15, func() float64 { return 0.999999 })
	if lo < time.Duration(float64(base)*0.85)-time.Millisecond || hi > time.Duration(float64(base)*1.15)+time.Millisecond {
		t.Fatalf("jitter out of ±15%%: %v .. %v", lo, hi)
	}
}

func TestActiveWindow(t *testing.T) {
	w, err := ParseWindow("06:30-22:30")
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("x", 0)
	if !w.Contains(time.Date(2026, 1, 1, 6, 30, 0, 0, loc)) || w.Contains(time.Date(2026, 1, 1, 22, 30, 0, 0, loc)) || w.Contains(time.Date(2026, 1, 1, 5, 0, 0, 0, loc)) {
		t.Fatal("window bounds wrong")
	}
}

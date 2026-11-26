package settings

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// memStore is an in-memory Store for tests.
type memStore struct{ data map[string]json.RawMessage }

func (m *memStore) GetSetting(_ context.Context, key string, into any) (bool, error) {
	raw, ok := m.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, into)
}

func (m *memStore) SetSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	m.data[key] = b
	return err
}

func TestLoadUsesDefaultsWhenUnset(t *testing.T) {
	svc, err := Load(context.Background(), &memStore{data: map[string]json.RawMessage{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.Get(); got.Timezone != "Asia/Jerusalem" || got.SyncFastInterval != 5*time.Minute {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestUpdateSavesAndNotifies(t *testing.T) {
	st := &memStore{data: map[string]json.RawMessage{}}
	svc, _ := Load(context.Background(), st)
	sub := svc.Subscribe()

	next, err := svc.Update(context.Background(), func(s *Settings) { s.PlanDefaultTime = "20:00" })
	if err != nil || next.PlanDefaultTime != "20:00" {
		t.Fatalf("Update = %+v, %v", next, err)
	}
	select {
	case got := <-sub:
		if got.PlanDefaultTime != "20:00" {
			t.Fatalf("subscriber got %q", got.PlanDefaultTime)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber not notified")
	}
	// Persisted: a fresh load sees the change.
	again, _ := Load(context.Background(), st)
	if again.Get().PlanDefaultTime != "20:00" {
		t.Fatal("change not persisted")
	}
}

func TestInvalidUpdateIsRejectedAndNothingChanges(t *testing.T) {
	st := &memStore{data: map[string]json.RawMessage{}}
	svc, _ := Load(context.Background(), st)
	_, err := svc.Update(context.Background(), func(s *Settings) { s.MashovMinGap = 10 * time.Millisecond })
	if err == nil {
		t.Fatal("expected validation error for min gap below 750ms")
	}
	if svc.Get().MashovMinGap != 750*time.Millisecond {
		t.Fatal("rejected update changed the live settings")
	}
	if _, ok := st.data[storeKey]; ok {
		t.Fatal("rejected update was persisted")
	}
}

func TestValidateRules(t *testing.T) {
	bad := []func(*Settings){
		func(s *Settings) { s.Timezone = "Mars/Olympus" },
		func(s *Settings) { s.MashovProfile = "ios" },
		func(s *Settings) { s.SyncFastInterval = 10 * time.Second },
		func(s *Settings) { s.ActiveHours = "25:00-22:30" },
		func(s *Settings) { s.PlanDefaultTime = "7pm" },
		func(s *Settings) { s.EventsRetention = time.Hour },
	}
	for i, mut := range bad {
		s := Defaults()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

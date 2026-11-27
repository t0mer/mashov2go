package mashovsource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

const storedDevicePass = `{"token":"abcdefghijklmnopqrstuvwxyz"}`

// fakeMashov serves the parent-scope endpoints the plan needs and counts logins.
type fakeMashov struct {
	mu             sync.Mutex
	passwordLogin  int
	deviceLogin    int
	revokeDevice   bool // loginDevice answers 400 (devicePass rejected)
	denyPassword   bool // login answers 400 Reason ChangePass
	lastDevicePass string
	paths          []string
}

func (f *fakeMashov) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/login":
		f.passwordLogin++
		if f.denyPassword {
			w.Header().Set("Reason", "ChangePass")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.writeLogin(w)
	case "/api/loginDevice":
		f.deviceLogin++
		f.lastDevicePass = r.Header.Get("DevicePass")
		if f.revokeDevice {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.writeLogin(w)
	case "/api/students/g1/timetable":
		writeJSON(w, `[{"timeTable":{"day":6,"groupId":10,"lesson":1,"roomNum":"12","weeks":-1},"groupDetails":{"groupId":10,"subjectName":"Math"}}]`)
	case "/api/students/g1/timetable/changes":
		writeJSON(w, `{"isEnabled":true,"changes":[],"cancellations":[]}`)
	case "/api/bells":
		writeJSON(w, `[{"lessonNumber":1,"startTime":"08:00:00","endTime":"08:45:00"}]`)
	case "/api/holidays":
		writeJSON(w, `[]`)
	case "/api/students/g1/homework":
		writeJSON(w, `[]`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeMashov) writeLogin(w http.ResponseWriter) {
	w.Header().Set("X-Csrf-Token", "csrf-test")
	w.Header().Set("DevicePass", storedDevicePass)
	writeJSON(w, `{"credential":{"semel":123456,"year":2027},"accessToken":{"displayName":"Parent","children":[{"childGuid":"g1","privateName":"Noa"}]}}`)
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func (f *fakeMashov) counts() (pw, dev int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.passwordLogin, f.deviceLogin
}

func setup(t *testing.T, fake *fakeMashov) (*Source, *store.Store, store.Account, family.Member) {
	t.Helper()
	ctx := context.Background()
	sealer, err := crypto.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "m.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	acc, err := st.CreateAccount(ctx, store.Account{Name: "home", Semel: 123456, Year: 2027,
		Username: "parent", Password: "pw-test", DeviceUUID: "uuid-test"})
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.CreateMember(ctx, childMember("Noa Levi"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LinkChild(ctx, kid.ID, acc.ID, "g1"); err != nil {
		t.Fatal(err)
	}
	src := &Source{Store: st, BaseURL: srv.URL + "/api/", MinGap: time.Microsecond}
	return src, st, acc, kid
}

func childMember(name string) family.Member {
	return family.Member{Role: family.RoleChild, Name: name, Language: "he", Channel: family.ChannelWhatsApp,
		WhatsAppPhone: "972500000001", QuietStart: "21:00", QuietEnd: "07:00", PlanEnabled: true, PlanTime: "19:30", Active: true}
}

var friday = time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)

func TestLoadLogsInWithPasswordAndStoresDevicePass(t *testing.T) {
	fake := &fakeMashov{}
	src, st, acc, kid := setup(t, fake)
	ctx := context.Background()

	in, first, err := src.Load(ctx, kid, friday)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if first != "Noa" {
		t.Fatalf("first name = %q", first)
	}
	if len(in.Lessons) != 1 || in.Lessons[0].GroupDetails.SubjectName != "Math" {
		t.Fatalf("lessons = %+v", in.Lessons)
	}
	if !in.IncludeHomeworkGivenToday || len(in.Bells) != 1 {
		t.Fatalf("input not fully loaded: %+v", in)
	}
	if pw, dev := fake.counts(); pw != 1 || dev != 0 {
		t.Fatalf("logins password=%d device=%d, want 1/0", pw, dev)
	}
	got, err := st.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DevicePass != storedDevicePass {
		t.Fatalf("devicePass not stored: %q", got.DevicePass)
	}

	// The session is reused: a second load does not log in again.
	if _, _, err := src.Load(ctx, kid, friday); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if pw, _ := fake.counts(); pw != 1 {
		t.Fatalf("second load logged in again: password logins = %d", pw)
	}
}

func TestLoadUsesStoredDevicePass(t *testing.T) {
	fake := &fakeMashov{}
	src, st, acc, kid := setup(t, fake)
	ctx := context.Background()
	if err := st.UpdateDevicePass(ctx, acc.ID, storedDevicePass); err != nil {
		t.Fatal(err)
	}

	if _, _, err := src.Load(ctx, kid, friday); err != nil {
		t.Fatalf("load: %v", err)
	}
	if pw, dev := fake.counts(); pw != 0 || dev != 1 {
		t.Fatalf("logins password=%d device=%d, want 0/1", pw, dev)
	}
	if fake.lastDevicePass != storedDevicePass {
		t.Fatal("loginDevice did not send the stored devicePass")
	}
}

func TestRevokedDevicePassFallsBackToPasswordOnce(t *testing.T) {
	fake := &fakeMashov{revokeDevice: true}
	src, st, acc, kid := setup(t, fake)
	ctx := context.Background()
	if err := st.UpdateDevicePass(ctx, acc.ID, storedDevicePass); err != nil {
		t.Fatal(err)
	}

	if _, _, err := src.Load(ctx, kid, friday); err != nil {
		t.Fatalf("load: %v", err)
	}
	if pw, dev := fake.counts(); pw != 1 || dev != 1 {
		t.Fatalf("logins password=%d device=%d, want 1/1", pw, dev)
	}
	got, _ := st.GetAccount(ctx, acc.ID)
	if got.DevicePass != storedDevicePass {
		t.Fatalf("fresh devicePass not stored after password login")
	}
}

func TestPasswordLoginIsNotRetriedWithinCooldown(t *testing.T) {
	fake := &fakeMashov{denyPassword: true}
	src, st, acc, kid := setup(t, fake)
	ctx := context.Background()

	if _, _, err := src.Load(ctx, kid, friday); err == nil {
		t.Fatal("expected the rejected login to fail")
	}
	got, _ := st.GetAccount(ctx, acc.ID)
	if got.AuthState != "needs_manual" {
		t.Fatalf("auth state = %q, want needs_manual", got.AuthState)
	}
	_, _, err := src.Load(ctx, kid, friday)
	if !errors.Is(err, ErrPasswordCooldown) {
		t.Fatalf("second load err = %v, want ErrPasswordCooldown", err)
	}
	if pw, _ := fake.counts(); pw != 1 {
		t.Fatalf("password logins = %d, want 1 (no retry inside the cooldown)", pw)
	}
}

func TestUnlinkedChildIsRejected(t *testing.T) {
	fake := &fakeMashov{}
	src, st, _, _ := setup(t, fake)
	ctx := context.Background()
	loose, err := st.CreateMember(ctx, childMember("Dana"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := src.Load(ctx, loose, friday); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("err = %v, want ErrNotLinked", err)
	}
	if pw, dev := fake.counts(); pw+dev != 0 {
		t.Fatal("an unlinked child must not reach Mashov")
	}
}

func TestFirstName(t *testing.T) {
	for in, want := range map[string]string{"Noa Levi": "Noa", "  Yoni ": "Yoni", "": ""} {
		if got := firstName(in); got != want {
			t.Errorf("firstName(%q) = %q, want %q", in, got, want)
		}
	}
}

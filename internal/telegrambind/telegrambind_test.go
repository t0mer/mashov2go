package telegrambind

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

const botToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// fakeBotAPI answers getUpdates with the queued batches, one per call, and
// records the offsets it was asked from.
type fakeBotAPI struct {
	mu      sync.Mutex
	batches [][]string // each batch is a list of raw update objects
	offsets []int64
}

func (f *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Offset int64 `json:"offset"`
	}
	_ = json.Unmarshal(body, &req)
	f.offsets = append(f.offsets, req.Offset)
	result := "[" + strings.Join(f.next(), ",") + "]"
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true,"result":`+result+`}`)
}

// next pops the next batch; an exhausted queue answers with an empty batch.
func (f *fakeBotAPI) next() []string {
	if len(f.batches) == 0 {
		return nil
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b
}

func update(id int64, chat int64, text string) string {
	return `{"update_id":` + itoa(id) + `,"message":{"text":"` + text + `","chat":{"id":` + itoa(chat) + `}}}`
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func setup(t *testing.T, api *fakeBotAPI) (*Service, *store.Store, family.Member) {
	t.Helper()
	ctx := context.Background()
	sealer, err := crypto.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "b.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	bot, err := connector.NewTelegram(botToken, srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Noa", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000001", QuietStart: "21:00", QuietEnd: "07:00",
		PlanEnabled: true, PlanTime: "19:30", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	if err := st.CreateBindToken(ctx, kid.ID, "0123456789abcdef0123456789abcdef", now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Store: st,
		Bot: func(context.Context) (int64, Updater, error) {
			return 7, bot, nil
		},
		Now: func() time.Time { return now },
	}
	return svc, st, kid
}

func TestBindsChatFromStartAndIgnoresTheRest(t *testing.T) {
	api := &fakeBotAPI{batches: [][]string{{
		update(10, 555, "/start 0123456789abcdef0123456789abcdef"),
		update(11, 666, "/start 0123456789abcdef0123456789abcdef"), // replay: already used
		update(12, 777, "/start fedcba9876543210fedcba9876543210"), // unknown
		update(13, 888, "hello"),                                   // not a start
	}}}
	svc, st, kid := setup(t, api)
	ctx := context.Background()

	n, err := svc.RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("bound = %d, err = %v; want 1", n, err)
	}
	got, _ := st.GetMember(ctx, kid.ID)
	if got.TelegramChatID != 555 {
		t.Fatalf("chat id = %d, want 555", got.TelegramChatID)
	}
}

func TestNextPollStartsAfterLastUpdate(t *testing.T) {
	api := &fakeBotAPI{batches: [][]string{{update(41, 555, "hi")}, {}}}
	svc, _, _ := setup(t, api)
	ctx := context.Background()

	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(api.offsets) != 2 || api.offsets[0] != 0 || api.offsets[1] != 42 {
		t.Fatalf("offsets = %v, want [0 42]", api.offsets)
	}
}

func TestNoBotIsNotAnError(t *testing.T) {
	svc := &Service{Bot: func(context.Context) (int64, Updater, error) { return 0, nil, ErrNoBot }}
	if n, err := svc.RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v, want 0 and nil", n, err)
	}
}

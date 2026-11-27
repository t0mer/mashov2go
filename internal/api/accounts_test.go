package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t0mer/mashov2go/internal/store"
)

// fakeMashovLogin answers POST login: "good" credentials get a student list and
// a devicePass, "suspended" gets Mashov's UserSuspended reason, anything else 400.
func fakeMashovLogin(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		switch body.Password {
		case "good password here":
			w.Header().Set("X-Csrf-Token", "csrf")
			w.Header().Set("DevicePass", `{"token":"abcdefghijklmnopqrstuvwxyz"}`)
			_, _ = io.WriteString(w, `{"credential":{"semel":1,"year":2027},"accessToken":{"displayName":"Parent",`+
				`"children":[{"childGuid":"g-noa","privateName":"Noa","familyName":"Levi","classCode":12}],"externChildren":[]}}`)
		case "suspended one":
			w.Header().Set("Reason", "UserSuspended")
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestCreateAccountChecksLoginAndListsStudents(t *testing.T) {
	mock := fakeMashovLogin(t)
	defer mock.Close()
	srv, h := newServer(t)
	srv.MashovBaseURL = mock.URL + "/api/"
	cookie, csrf := signIn(t, h)

	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/accounts",
		map[string]any{"name": "home", "semel": 1, "year": 2027, "username": "parent", "password": "good password here"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "good password here") || strings.Contains(rec.Body.String(), "abcdefghijklmnop") {
		t.Fatal("secret echoed in the response")
	}
	var out struct {
		Account  accountView   `json:"account"`
		Students []studentView `json:"students"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Account.AuthState != "ok" || len(out.Students) != 1 || out.Students[0].ChildGUID != "g-noa" || out.Students[0].Name != "Noa" {
		t.Fatalf("response: %+v", out)
	}

	// The devicePass and password are stored sealed.
	stored, err := srv.Store.GetAccount(t.Context(), out.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Password != "good password here" || stored.DevicePass == "" || stored.DeviceUUID == "" {
		t.Fatalf("stored account incomplete: %+v", stored)
	}

	list := call(h, http.MethodGet, "/api/v1/accounts", nil, cookie, nil)
	if strings.Contains(list.Body.String(), "good password here") || !strings.Contains(list.Body.String(), `"username":"parent"`) {
		t.Fatalf("list: %s", list.Body.String())
	}

	again := adminCall(h, cookie, csrf, http.MethodGet, "/api/v1/accounts/"+itoa(out.Account.ID)+"/students", nil)
	if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), "g-noa") {
		t.Fatalf("students: %d %s", again.Code, again.Body.String())
	}
}

func TestFailedMashovLoginIsNotStored(t *testing.T) {
	mock := fakeMashovLogin(t)
	defer mock.Close()
	srv, h := newServer(t)
	srv.MashovBaseURL = mock.URL + "/api/"
	cookie, csrf := signIn(t, h)

	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/accounts",
		map[string]any{"name": "home", "semel": 1, "year": 2027, "username": "parent", "password": "suspended one"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "needs_manual") {
		t.Fatalf("suspended: %d %s", rec.Code, rec.Body.String())
	}
	list, _ := srv.Store.ListAccounts(t.Context())
	if len(list) != 0 {
		t.Fatalf("failed login stored %d accounts", len(list))
	}
}

func TestAccountStudentsRecordsFailedLogin(t *testing.T) {
	mock := fakeMashovLogin(t)
	defer mock.Close()
	srv, h := newServer(t)
	srv.MashovBaseURL = mock.URL + "/api/"
	cookie, csrf := signIn(t, h)
	acc, err := srv.Store.CreateAccount(t.Context(), store.Account{Name: "home", Semel: 1, Year: 2027,
		Username: "parent", Password: "wrong password here", DeviceUUID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	rec := adminCall(h, cookie, csrf, http.MethodGet, "/api/v1/accounts/"+itoa(acc.ID)+"/students", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("students with wrong password: %d", rec.Code)
	}
}

func TestAccountValidation(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	cases := map[string]map[string]any{
		"no name":     {"semel": 1, "year": 2027, "username": "u", "password": "p"},
		"no semel":    {"name": "x", "year": 2027, "username": "u", "password": "p"},
		"no password": {"name": "x", "semel": 1, "year": 2027, "username": "u"},
	}
	for name, body := range cases {
		if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/accounts", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}
}

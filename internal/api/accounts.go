package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/t0mer/mashov2go/internal/auth"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/store"
)

// accountView is an account without its secrets.
type accountView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Semel     int    `json:"semel"`
	Year      int    `json:"year"`
	Username  string `json:"username"`
	AuthState string `json:"authState"`
}

func viewAccount(a store.Account) accountView {
	return accountView{ID: a.ID, Name: a.Name, Semel: a.Semel, Year: a.Year, Username: a.Username, AuthState: a.AuthState}
}

// studentView is a Mashov student the account can see, for linking to a child.
type studentView struct {
	ChildGUID  string          `json:"childGuid"`
	Name       string          `json:"name"`
	FamilyName string          `json:"familyName"`
	ClassCode  json.RawMessage `json:"classCode,omitempty"`
}

func viewStudents(children []mashov.Child) []studentView {
	out := make([]studentView, 0, len(children))
	for _, c := range children {
		out = append(out, studentView{ChildGUID: c.ChildGUID, Name: c.PrivateName, FamilyName: c.FamilyName, ClassCode: c.ClassCode})
	}
	return out
}

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/accounts", s.authed("read", http.HandlerFunc(s.listAccounts)))
	mux.Handle("POST /api/v1/accounts", s.authed("admin", http.HandlerFunc(s.createAccount)))
	mux.Handle("GET /api/v1/accounts/{id}/students", s.authed("admin", http.HandlerFunc(s.accountStudents)))
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListAccounts(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing accounts failed")
		return
	}
	out := make([]accountView, 0, len(list))
	for _, a := range list {
		out = append(out, viewAccount(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// mashovLogin signs in to Mashov with a password. Each call is one attempt.
func (s *Server) mashovLogin(ctx context.Context, semel, year int, username, password, deviceUUID string) (*mashov.LoginResponse, error) {
	c, err := mashov.NewClient(mashov.Options{BaseURL: s.MashovBaseURL, Transport: s.MashovTransport, Profile: mashov.AndroidProfile})
	if err != nil {
		return nil, err
	}
	return c.Login(ctx, mashov.LoginRequest{
		Semel: semel, Year: year, Username: username, Password: password,
		IsPrivateDevice: true, DeviceFields: mashov.AndroidProfile.DeviceFields(deviceUUID),
	}, "")
}

// writeLoginFailure maps a failed Mashov login to a response the UI can act on.
// The state is one of the CLAUDE.md §3.2 states; no credentials are echoed.
func writeLoginFailure(w http.ResponseWriter, err error) {
	var le *mashov.LoginError
	if errors.As(err, &le) {
		problem(w, http.StatusUnprocessableEntity, "Mashov sign-in failed", "state: "+string(le.State))
		return
	}
	problem(w, http.StatusBadGateway, "Mashov unreachable", "could not reach Mashov; try again later")
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Semel    int    `json:"semel"`
		Year     int    `json:"year"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name, in.Username = strings.TrimSpace(in.Name), strings.TrimSpace(in.Username)
	switch {
	case in.Name == "" || len(in.Name) > 64:
		problem(w, http.StatusBadRequest, "Bad request", "name must be 1 to 64 characters")
		return
	case in.Semel <= 0 || in.Year <= 0:
		problem(w, http.StatusBadRequest, "Bad request", "semel and year are required")
		return
	case in.Username == "" || in.Password == "":
		problem(w, http.StatusBadRequest, "Bad request", "username and password are required")
		return
	}
	deviceUUID, err := auth.NewUUID()
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating the device identity failed")
		return
	}
	res, err := s.mashovLogin(r.Context(), in.Semel, in.Year, in.Username, in.Password, deviceUUID)
	if err != nil {
		writeLoginFailure(w, err)
		return
	}
	acc, err := s.Store.CreateAccount(r.Context(), store.Account{
		Name: in.Name, Semel: in.Semel, Year: in.Year, Username: in.Username, Password: in.Password,
		DevicePass: res.DevicePass, DeviceUUID: deviceUUID, AuthState: "ok",
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the account failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"account":  viewAccount(acc),
		"students": viewStudents(res.AccessToken.Children),
	})
}

// accountStudents signs in again with the stored credentials and lists the
// students. It records the outcome as the account's auth state.
func (s *Server) accountStudents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the id is not valid")
		return
	}
	acc, err := s.Store.GetAccount(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		problem(w, http.StatusNotFound, "Not found", "no such account")
		return
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "loading the account failed")
		return
	}
	res, err := s.mashovLogin(r.Context(), acc.Semel, acc.Year, acc.Username, acc.Password, acc.DeviceUUID)
	if err != nil {
		var le *mashov.LoginError
		if errors.As(err, &le) {
			_ = s.Store.SetAuthState(r.Context(), acc.ID, string(le.State))
		}
		writeLoginFailure(w, err)
		return
	}
	if res.DevicePass != "" {
		_ = s.Store.UpdateDevicePass(r.Context(), acc.ID, res.DevicePass)
	}
	if acc.AuthState != "ok" {
		_ = s.Store.SetAuthState(r.Context(), acc.ID, "ok")
	}
	writeJSON(w, http.StatusOK, viewStudents(res.AccessToken.Children))
}

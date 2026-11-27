package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/store"
)

// connectorView is a connector without its config. Secrets are write-only.
type connectorView struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}

func viewConnector(c store.Connector) connectorView {
	return connectorView{ID: c.ID, Type: c.Type, Name: c.Name, IsDefault: c.IsDefault, Enabled: c.Enabled}
}

func (s *Server) connectorRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/connectors", s.authed("read", http.HandlerFunc(s.listConnectors)))
	mux.Handle("POST /api/v1/connectors", s.authed("admin", http.HandlerFunc(s.createConnector)))
	mux.Handle("PATCH /api/v1/connectors/{id}", s.authed("admin", http.HandlerFunc(s.updateConnector)))
	mux.Handle("DELETE /api/v1/connectors/{id}", s.authed("admin", http.HandlerFunc(s.deleteConnector)))
	mux.Handle("POST /api/v1/connectors/{id}/default", s.authed("admin", http.HandlerFunc(s.setDefaultConnector)))
	mux.Handle("GET /api/v1/connectors/{id}/health", s.authed("read", http.HandlerFunc(s.connectorHealth)))
	mux.Handle("POST /api/v1/connectors/{id}/test", s.authed("admin", http.HandlerFunc(s.testConnector)))
}

func (s *Server) listConnectors(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListConnectors(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing connectors failed")
		return
	}
	out := make([]connectorView, 0, len(list))
	for _, c := range list {
		out = append(out, viewConnector(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// buildConnector validates a config by building the connector. Errors from the
// connector package never include secret values.
func buildConnector(typ string, cfg []byte) (connector.Connector, error) {
	switch typ {
	case "greenapi", "openwa", "telegram":
		return connector.Build(typ, cfg, nil)
	default:
		return nil, errors.New("type must be greenapi, openwa or telegram")
	}
}

func (s *Server) createConnector(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		IsDefault bool            `json:"isDefault"`
		Config    json.RawMessage `json:"config"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		problem(w, http.StatusBadRequest, "Bad request", "name must be 1 to 64 characters")
		return
	}
	if len(in.Config) == 0 {
		problem(w, http.StatusBadRequest, "Bad request", "config is required")
		return
	}
	c, err := buildConnector(in.Type, in.Config)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	_ = c.Close()
	if in.IsDefault && in.Type == "telegram" {
		problem(w, http.StatusBadRequest, "Bad request", "a telegram connector cannot be the default WhatsApp connector")
		return
	}
	created, err := s.Store.CreateConnector(r.Context(),
		store.Connector{Type: in.Type, Name: name, IsDefault: in.IsDefault, Enabled: true}, in.Config)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the connector failed")
		return
	}
	writeJSON(w, http.StatusCreated, viewConnector(created))
}

// loadConnector returns the connector named in the path.
func (s *Server) loadConnector(w http.ResponseWriter, r *http.Request) (store.Connector, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the id is not valid")
		return store.Connector{}, false
	}
	c, found, err := s.Store.GetConnector(r.Context(), id)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "loading the connector failed")
		return store.Connector{}, false
	}
	if !found {
		problem(w, http.StatusNotFound, "Not found", "no such connector")
		return store.Connector{}, false
	}
	return c, true
}

// updateConnector changes the name, the enabled flag, or (when config is sent)
// the whole config. A new config is validated before it is stored.
func (s *Server) updateConnector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnector(w, r)
	if !ok {
		return
	}
	var in struct {
		Name    *string         `json:"name"`
		Enabled *bool           `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := c.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" || len(name) > 64 {
		problem(w, http.StatusBadRequest, "Bad request", "name must be 1 to 64 characters")
		return
	}
	enabled := c.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var cfg []byte
	if len(in.Config) > 0 {
		built, err := buildConnector(c.Type, in.Config)
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		_ = built.Close()
		cfg = in.Config
	}
	if err := s.Store.UpdateConnector(r.Context(), c.ID, name, enabled, cfg); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the connector failed")
		return
	}
	c.Name, c.Enabled = name, enabled
	writeJSON(w, http.StatusOK, viewConnector(c))
}

func (s *Server) setDefaultConnector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnector(w, r)
	if !ok {
		return
	}
	if err := s.Store.SetDefaultConnector(r.Context(), c.ID); err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	c.IsDefault = true
	writeJSON(w, http.StatusOK, viewConnector(c))
}

func (s *Server) deleteConnector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnector(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteConnector(r.Context(), c.ID); err != nil {
		if errors.Is(err, store.ErrConnectorInUse) {
			problem(w, http.StatusConflict, "Connector in use", "members still use this connector; reassign them first")
			return
		}
		problem(w, http.StatusInternalServerError, "Internal error", "deleting the connector failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openConnector builds a stored connector from its sealed config.
func (s *Server) openConnector(ctx context.Context, c store.Connector) (connector.Connector, error) {
	cfg, err := s.Store.ConnectorConfig(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return connector.Build(c.Type, cfg, nil)
}

func (s *Server) connectorHealth(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnector(w, r)
	if !ok {
		return
	}
	conn, err := s.openConnector(r.Context(), c)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "the connector cannot be opened")
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": string(conn.Health(ctx))})
}

// testConnector sends a test message to one family member through the connector.
// WhatsApp connectors use the member's phone; Telegram uses the bound chat.
func (s *Server) testConnector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadConnector(w, r)
	if !ok {
		return
	}
	var in struct {
		MemberID int64 `json:"memberId"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Store.GetMember(r.Context(), in.MemberID)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "no such member")
		return
	}
	var address string
	switch c.Type {
	case "telegram":
		if m.TelegramChatID == 0 {
			problem(w, http.StatusBadRequest, "Bad request", "this member has no bound Telegram chat")
			return
		}
		address = strconv.FormatInt(m.TelegramChatID, 10)
	default:
		if m.WhatsAppPhone == "" {
			problem(w, http.StatusBadRequest, "Bad request", "this member has no phone number")
			return
		}
		address = m.WhatsAppPhone
	}
	conn, err := s.openConnector(r.Context(), c)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "the connector cannot be opened")
		return
	}
	defer conn.Close()
	if err := conn.SendTo(r.Context(), address, connector.Message{Text: "mashov2go test message"}); err != nil {
		if connector.IsPermanent(err) {
			problem(w, http.StatusBadGateway, "Send rejected", "the provider rejected the message")
			return
		}
		problem(w, http.StatusBadGateway, "Send failed", "the message could not be sent; try again later")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

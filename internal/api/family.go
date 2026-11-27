package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

// memberInput is the body for create (POST) and partial update (PATCH). A nil
// field is left unchanged on update and takes the role default on create.
type memberInput struct {
	Name                 *string `json:"name"`
	Language             *string `json:"language"`
	WhatsAppPhone        *string `json:"whatsappPhone"`
	Channel              *string `json:"channel"`
	WhatsAppConnectorID  *int64  `json:"whatsappConnectorId"`
	FallbackChannel      *string `json:"fallbackChannel"`
	QuietStart           *string `json:"quietStart"`
	QuietEnd             *string `json:"quietEnd"`
	PlanEnabled          *bool   `json:"planEnabled"`
	PlanTime             *string `json:"planTime"`
	IncludeHomeworkToday *bool   `json:"includeHomeworkToday"`
	Active               *bool   `json:"active"`
}

// memberView is what the API returns. Secrets never appear here; the Telegram
// chat id is shown only as bound or not, because it identifies a person.
type memberView struct {
	ID                   int64     `json:"id"`
	Role                 string    `json:"role"`
	Name                 string    `json:"name"`
	Language             string    `json:"language"`
	WhatsAppPhone        string    `json:"whatsappPhone"`
	TelegramBound        bool      `json:"telegramBound"`
	Channel              string    `json:"channel"`
	WhatsAppConnectorID  int64     `json:"whatsappConnectorId"`
	FallbackChannel      string    `json:"fallbackChannel"`
	QuietStart           string    `json:"quietStart"`
	QuietEnd             string    `json:"quietEnd"`
	PlanEnabled          bool      `json:"planEnabled"`
	PlanTime             string    `json:"planTime"`
	IncludeHomeworkToday bool      `json:"includeHomeworkToday"`
	Active               bool      `json:"active"`
	Link                 *linkView `json:"link,omitempty"`
}

type linkView struct {
	AccountID int64  `json:"accountId"`
	ChildGUID string `json:"childGuid"`
}

func viewMember(m family.Member) memberView {
	return memberView{
		ID: m.ID, Role: m.Role, Name: m.Name, Language: m.Language, WhatsAppPhone: m.WhatsAppPhone,
		TelegramBound: m.TelegramChatID != 0, Channel: m.Channel, WhatsAppConnectorID: m.WhatsAppConnectorID,
		FallbackChannel: m.FallbackChannel, QuietStart: m.QuietStart, QuietEnd: m.QuietEnd,
		PlanEnabled: m.PlanEnabled, PlanTime: m.PlanTime, IncludeHomeworkToday: m.IncludeHomeworkToday,
		Active: m.Active,
	}
}

// familyRoutes registers the member routes for one role under base.
func (s *Server) familyRoutes(mux *http.ServeMux, base, role string) {
	mux.Handle("GET "+base, s.authed("read", s.listMembers(role)))
	mux.Handle("POST "+base, s.authed("admin", s.createMember(role)))
	mux.Handle("GET "+base+"/{id}", s.authed("read", s.getMember(role)))
	mux.Handle("PATCH "+base+"/{id}", s.authed("admin", s.patchMember(role)))
	mux.Handle("DELETE "+base+"/{id}", s.authed("admin", s.deleteMember(role)))
	if role == family.RoleChild {
		s.planRoutes(mux, base)
		mux.Handle("POST "+base+"/{id}/link", s.authed("admin", s.linkChild()))
		mux.Handle("DELETE "+base+"/{id}/link", s.authed("admin", s.unlinkChild()))
		mux.Handle("GET "+base+"/{id}/recipients", s.authed("read", s.getRecipients()))
		mux.Handle("PUT "+base+"/{id}/recipients", s.authed("admin", s.putRecipients()))
		mux.Handle("POST "+base+"/{id}/recipients/reset", s.authed("admin", s.resetRecipients()))
	}
}

func (s *Server) listMembers(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		all, err := s.Store.ListMembers(r.Context())
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "listing members failed")
			return
		}
		out := []memberView{}
		for _, m := range all {
			if m.Role == role {
				out = append(out, viewMember(m))
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// loadMember returns the member named in the path when it has the expected role.
func (s *Server) loadMember(w http.ResponseWriter, r *http.Request, role string) (family.Member, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the id is not valid")
		return family.Member{}, false
	}
	m, err := s.Store.GetMember(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && m.Role != role) {
		problem(w, http.StatusNotFound, "Not found", "no such member")
		return family.Member{}, false
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "loading the member failed")
		return family.Member{}, false
	}
	return m, true
}

func (s *Server) getMember(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, role)
		if !ok {
			return
		}
		v := viewMember(m)
		if role == family.RoleChild {
			if accID, guid, linked, err := s.Store.MemberLink(r.Context(), m.ID); err != nil {
				problem(w, http.StatusInternalServerError, "Internal error", "loading the link failed")
				return
			} else if linked {
				v.Link = &linkView{AccountID: accID, ChildGUID: guid}
			}
		}
		writeJSON(w, http.StatusOK, v)
	})
}

// roleDefaults are the starting values for a new member (CLAUDE.md §6.1).
func roleDefaults(role string) family.Member {
	m := family.Member{
		Role: role, Language: "he", Channel: family.ChannelWhatsApp,
		PlanEnabled: true, PlanTime: "19:30", IncludeHomeworkToday: true, Active: true,
	}
	if role == family.RoleChild {
		m.QuietStart, m.QuietEnd = "21:00", "07:00"
	} else {
		m.QuietStart, m.QuietEnd = "22:30", "06:30"
	}
	return m
}

func (s *Server) createMember(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in memberInput
		if !decode(w, r, &in) {
			return
		}
		m := roleDefaults(role)
		if err := s.applyMemberInput(r.Context(), &m, in); err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		if err := m.Validate(); err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		created, err := s.Store.CreateMember(r.Context(), m)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "saving the member failed")
			return
		}
		writeJSON(w, http.StatusCreated, viewMember(created))
	})
}

func (s *Server) patchMember(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, role)
		if !ok {
			return
		}
		var in memberInput
		if !decode(w, r, &in) {
			return
		}
		if err := s.applyMemberInput(r.Context(), &m, in); err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		if err := m.Validate(); err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		if err := s.Store.UpdateMember(r.Context(), m); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "saving the member failed")
			return
		}
		writeJSON(w, http.StatusOK, viewMember(m))
	})
}

func (s *Server) deleteMember(role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, role)
		if !ok {
			return
		}
		if err := s.Store.DeleteMember(r.Context(), m.ID); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "deleting the member failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// applyMemberInput copies the set fields onto m. Phones are normalised and a
// WhatsApp connector id must name an existing connector.
func (s *Server) applyMemberInput(ctx context.Context, m *family.Member, in memberInput) error {
	if in.Name != nil {
		m.Name = strings.TrimSpace(*in.Name)
	}
	if in.Language != nil {
		m.Language = *in.Language
	}
	if in.WhatsAppPhone != nil {
		p, err := family.NormalizePhone(*in.WhatsAppPhone)
		if err != nil {
			return fmt.Errorf("whatsappPhone: %w", err)
		}
		m.WhatsAppPhone = p
	}
	if in.Channel != nil {
		m.Channel = *in.Channel
	}
	if in.FallbackChannel != nil {
		m.FallbackChannel = *in.FallbackChannel
	}
	if in.WhatsAppConnectorID != nil {
		if *in.WhatsAppConnectorID != 0 {
			if _, found, err := s.Store.GetConnector(ctx, *in.WhatsAppConnectorID); err != nil {
				return err
			} else if !found {
				return errors.New("whatsappConnectorId: no such connector")
			}
		}
		m.WhatsAppConnectorID = *in.WhatsAppConnectorID
	}
	if in.QuietStart != nil {
		m.QuietStart = *in.QuietStart
	}
	if in.QuietEnd != nil {
		m.QuietEnd = *in.QuietEnd
	}
	if in.PlanEnabled != nil {
		m.PlanEnabled = *in.PlanEnabled
	}
	if in.PlanTime != nil {
		m.PlanTime = *in.PlanTime
	}
	if in.IncludeHomeworkToday != nil {
		m.IncludeHomeworkToday = *in.IncludeHomeworkToday
	}
	if in.Active != nil {
		m.Active = *in.Active
	}
	return nil
}

// linkChild ties a child to a Mashov student on a stored account. The student
// is identified by the child GUID that Mashov lists for that account.
func (s *Server) linkChild() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		var in struct {
			AccountID int64  `json:"accountId"`
			ChildGUID string `json:"childGuid"`
		}
		if !decode(w, r, &in) {
			return
		}
		guid := strings.TrimSpace(in.ChildGUID)
		if guid == "" || len(guid) > 64 {
			problem(w, http.StatusBadRequest, "Bad request", "childGuid must be 1 to 64 characters")
			return
		}
		if _, err := s.Store.GetAccount(r.Context(), in.AccountID); err != nil {
			problem(w, http.StatusBadRequest, "Bad request", "no such account")
			return
		}
		if _, _, linked, err := s.Store.MemberLink(r.Context(), m.ID); err != nil || linked {
			if err != nil {
				problem(w, http.StatusInternalServerError, "Internal error", "checking the link failed")
				return
			}
			problem(w, http.StatusConflict, "Conflict", "this child is already linked")
			return
		}
		if _, err := s.Store.ChildMemberFor(r.Context(), in.AccountID, guid); err == nil {
			problem(w, http.StatusConflict, "Conflict", "this Mashov student is already linked to a child")
			return
		}
		if err := s.Store.LinkChild(r.Context(), m.ID, in.AccountID, guid); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "saving the link failed")
			return
		}
		writeJSON(w, http.StatusOK, viewMember(m))
	})
}

func (s *Server) unlinkChild() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if err := s.Store.UnlinkChild(r.Context(), m.ID); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "removing the link failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// effectiveRecipients merges stored overrides over the defaults for every category.
func (s *Server) effectiveRecipients(ctx context.Context, childID int64) (map[string]string, error) {
	stored, err := s.Store.Recipients(ctx, childID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range family.Categories {
		r := family.DefaultRecipients[c]
		if o, ok := stored[c]; ok {
			r = o
		}
		out[c] = string(r)
	}
	return out, nil
}

func (s *Server) getRecipients() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		out, err := s.effectiveRecipients(r.Context(), m.ID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "loading recipients failed")
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// putRecipients sets overrides. Categories and values are checked; a category
// left out keeps its current value.
func (s *Server) putRecipients() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		var in map[string]string
		if !decode(w, r, &in) {
			return
		}
		known := map[string]bool{}
		for _, c := range family.Categories {
			known[c] = true
		}
		for c, v := range in {
			if !known[c] {
				problem(w, http.StatusBadRequest, "Bad request", fmt.Sprintf("unknown category %q", c))
				return
			}
			if !family.ValidRecipient(family.Recipient(v)) {
				problem(w, http.StatusBadRequest, "Bad request", fmt.Sprintf("recipient for %q must be child, parents, both or off", c))
				return
			}
		}
		for c, v := range in {
			if err := s.Store.SetRecipient(r.Context(), m.ID, c, family.Recipient(v)); err != nil {
				problem(w, http.StatusInternalServerError, "Internal error", "saving recipients failed")
				return
			}
		}
		out, err := s.effectiveRecipients(r.Context(), m.ID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "loading recipients failed")
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (s *Server) resetRecipients() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if err := s.Store.ResetRecipients(r.Context(), m.ID); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "resetting recipients failed")
			return
		}
		out, err := s.effectiveRecipients(r.Context(), m.ID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "loading recipients failed")
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

package api

import (
	"fmt"
	"net/http"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

type muteView struct {
	ChildID  int64  `json:"childId"`
	Category string `json:"category"`
}

func (s *Server) muteRoutes(mux *http.ServeMux, base string) {
	mux.Handle("GET "+base+"/{id}/mutes", s.authed("read", s.listMutes()))
	mux.Handle("PUT "+base+"/{id}/mutes", s.authed("admin", s.replaceMutes()))
}

func (s *Server) listMutes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.loadMember(w, r, family.RoleParent)
		if !ok {
			return
		}
		list, err := s.Store.ListMutes(r.Context(), p.ID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "listing mutes failed")
			return
		}
		out := make([]muteView, 0, len(list))
		for _, m := range list {
			out = append(out, muteView{ChildID: m.ChildID, Category: m.Category})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// replaceMutes sets the parent's mutes to the list sent. Every child must be an
// active child member and every category must be a known one.
func (s *Server) replaceMutes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.loadMember(w, r, family.RoleParent)
		if !ok {
			return
		}
		var in struct {
			Mutes []muteView `json:"mutes"`
		}
		if !decode(w, r, &in) {
			return
		}
		known := map[string]bool{}
		for _, c := range family.Categories {
			known[c] = true
		}
		mutes := make([]store.Mute, 0, len(in.Mutes))
		for _, m := range in.Mutes {
			if !known[m.Category] {
				problem(w, http.StatusBadRequest, "Bad request", fmt.Sprintf("unknown category %q", m.Category))
				return
			}
			child, err := s.Store.GetMember(r.Context(), m.ChildID)
			if err != nil || child.Role != family.RoleChild {
				problem(w, http.StatusBadRequest, "Bad request", fmt.Sprintf("child %d does not exist", m.ChildID))
				return
			}
			mutes = append(mutes, store.Mute{ChildID: m.ChildID, Category: m.Category})
		}
		if err := s.Store.ReplaceMutes(r.Context(), p.ID, mutes); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "saving mutes failed")
			return
		}
		s.listMutes().ServeHTTP(w, r)
	})
}

package api

import (
	"errors"
	"net/http"

	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/templates"
)

type templateView struct {
	Category  string `json:"category"`
	Language  string `json:"language"`
	Audience  string `json:"audience"`
	Body      string `json:"body"`
	IsDefault bool   `json:"isDefault"`
}

func (s *Server) templateRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/v1/templates", s.authed("read", http.HandlerFunc(s.listTemplates)))
	s.handle(mux, "PUT /api/v1/templates", s.authed("admin", http.HandlerFunc(s.putTemplate)))
	s.handle(mux, "DELETE /api/v1/templates/{category}/{language}/{audience}", s.authed("admin", http.HandlerFunc(s.resetTemplate)))
	s.handle(mux, "POST /api/v1/templates/preview", s.authed("admin", http.HandlerFunc(s.previewTemplate)))
}

// listTemplates returns every template key with the body in effect: the override
// when one is stored, otherwise the built-in default.
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	overrides, err := s.Store.ListTemplates(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing templates failed")
		return
	}
	byKey := map[string]store.Template{}
	for _, o := range overrides {
		byKey[o.Category+"|"+o.Language+"|"+o.Audience] = o
	}
	out := []templateView{}
	for _, c := range templates.Categories {
		for _, lang := range templates.Languages {
			for _, aud := range templates.Audiences {
				def, ok := templates.Default(c, lang, aud)
				if !ok {
					continue
				}
				v := templateView{Category: c, Language: lang, Audience: aud, Body: def, IsDefault: true}
				if o, found := byKey[c+"|"+lang+"|"+aud]; found {
					v.Body, v.IsDefault = o.Body, false
				}
				out = append(out, v)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// checkKey validates a template key.
func checkKey(category, language, audience string) error {
	known := false
	for _, c := range templates.Categories {
		if c == category {
			known = true
		}
	}
	if !known {
		return errors.New("category has no template")
	}
	if !inList(templates.Languages, language) || !inList(templates.Audiences, audience) {
		return errors.New("language must be he or en, audience child or parent")
	}
	if _, ok := templates.Default(category, language, audience); !ok {
		return errors.New("this category has no template for that language and audience")
	}
	return nil
}

func inList(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// putTemplate saves an override after it parses and renders with sample data.
func (s *Server) putTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateView
	if !decode(w, r, &in) {
		return
	}
	if err := checkKey(in.Category, in.Language, in.Audience); err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if err := validateBody(in.Category, in.Body); err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if err := s.Store.PutTemplate(r.Context(), store.Template{Category: in.Category, Language: in.Language,
		Audience: in.Audience, Body: in.Body}, s.now()); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the template failed")
		return
	}
	in.IsDefault = false
	writeJSON(w, http.StatusOK, in)
}

// validateBody parses the body and renders it once with sample data.
func validateBody(category, body string) error {
	t, err := templates.Parse(body)
	if err != nil {
		return err
	}
	_, err = templates.Execute(t, templates.SampleData(category))
	return err
}

// resetTemplate removes an override so the default applies again.
func (s *Server) resetTemplate(w http.ResponseWriter, r *http.Request) {
	c, l, a := r.PathValue("category"), r.PathValue("language"), r.PathValue("audience")
	if err := checkKey(c, l, a); err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if err := s.Store.DeleteTemplate(r.Context(), c, l, a); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "resetting the template failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewTemplate renders a body (not necessarily saved) with sample data.
func (s *Server) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateView
	if !decode(w, r, &in) {
		return
	}
	if !inList(templates.Categories, in.Category) {
		problem(w, http.StatusBadRequest, "Bad request", "category has no template")
		return
	}
	t, err := templates.Parse(in.Body)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	text, err := templates.Execute(t, templates.SampleData(in.Category))
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

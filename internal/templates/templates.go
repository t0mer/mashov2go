// Package templates renders the family messages from Go text/template bodies
// (CLAUDE.md §7). Each category has a built-in default per language and audience.
// The admin can override a default; an override is validated before it is stored.
package templates

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/t0mer/mashov2go/internal/store"
)

// Languages and audiences a template can be written for.
var (
	Languages = []string{"he", "en"}
	Audiences = []string{"child", "parent"}
)

// Categories that have a template, with the audience each one is used for by default.
var Categories = []string{"grades", "behave", "homework"}

// defaults are the built-in bodies, keyed by category, language and audience.
var defaults = map[string]string{
	key("grades", "he", "parent"): `{{if .updated}}📊 ציון עודכן{{else}}📊 ציון חדש{{end}} – {{.child}}
{{.subject}}{{with .event}} · {{.}}{{end}}
ציון: {{.value}}`,
	key("grades", "en", "parent"): `{{if .updated}}📊 Grade updated{{else}}📊 New grade{{end}} – {{.child}}
{{.subject}}{{with .event}} · {{.}}{{end}}
Grade: {{.value}}`,
	key("behave", "he", "parent"): `⚠️ {{.name}} – {{.child}}
{{.subject}} · שיעור {{.lesson}}{{with .date}} · {{.}}{{end}}{{with .justification}}
{{.}}{{end}}`,
	key("behave", "en", "parent"): `⚠️ {{.name}} – {{.child}}
{{.subject}} · lesson {{.lesson}}{{with .date}} · {{.}}{{end}}{{with .justification}}
{{.}}{{end}}`,
	key("homework", "he", "child"): `📚 שיעורי בית חדשים – {{.subject}}
{{.text}}{{with .remark}}
{{.}}{{end}}{{with .date}}
(ניתן {{.}}){{end}}`,
	key("homework", "en", "child"): `📚 New homework – {{.subject}}
{{.text}}{{with .remark}}
{{.}}{{end}}{{with .date}}
(given {{.}}){{end}}`,
}

func key(category, lang, audience string) string {
	return category + "|" + lang + "|" + audience
}

// Default returns the built-in body for a key, if there is one.
func Default(category, lang, audience string) (string, bool) {
	body, ok := defaults[key(category, lang, audience)]
	return body, ok
}

// Parse compiles a template body. Missing fields are an error, so a typo in an
// override is caught on save instead of producing a blank message.
func Parse(body string) (*template.Template, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("the template is empty")
	}
	if utf8.RuneCountInString(body) > 2000 {
		return nil, fmt.Errorf("the template is longer than 2000 characters")
	}
	t, err := template.New("m").Option("missingkey=error").Funcs(funcs).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	return t, nil
}

var funcs = template.FuncMap{
	// trim cuts a string to n runes, for long homework or messages.
	"trim": func(n int, s string) string {
		if utf8.RuneCountInString(s) <= n {
			return s
		}
		return string([]rune(s)[:n]) + "…"
	},
}

// Execute renders a compiled body with data.
func Execute(t *template.Template, data map[string]any) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("rendering: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}

// Store is the part of the store the renderer reads overrides from.
type Store interface {
	GetTemplate(ctx context.Context, category, language, audience string) (store.Template, bool, error)
}

// Renderer renders a message with the stored override when there is one, and
// the built-in default otherwise.
type Renderer struct {
	Store Store
}

func otherAudience(a string) string {
	if a == "child" {
		return "parent"
	}
	return "child"
}

// Render returns the message text for category, language and audience.
func (r Renderer) Render(ctx context.Context, category, lang, audience string, data map[string]any) (string, error) {
	aud := audience
	if _, ok := Default(category, lang, aud); !ok {
		// Some categories have one audience only (homework is for the child). A
		// recipient of the other audience gets that template instead of nothing.
		aud = otherAudience(audience)
		if _, ok := Default(category, lang, aud); !ok {
			return "", fmt.Errorf("no template for %s/%s", category, lang)
		}
	}
	body, _ := Default(category, lang, aud)
	if r.Store != nil {
		o, found, err := r.Store.GetTemplate(ctx, category, lang, aud)
		if err != nil {
			return "", err
		}
		if found {
			body = o.Body
		}
	}
	t, err := Parse(body)
	if err != nil {
		return "", err
	}
	return Execute(t, data)
}

// SampleData returns fixed example data for a category, used by the preview so
// an admin can see a template before it is used.
func SampleData(category string) map[string]any {
	switch category {
	case "grades":
		return map[string]any{"child": "Demo Child", "subject": "Math", "event": "Quiz", "value": "92", "updated": false}
	case "behave":
		return map[string]any{"child": "Demo Child", "name": "איחור", "subject": "Math", "lesson": 3,
			"date": "02/11", "justification": ""}
	case "homework":
		return map[string]any{"subject": "Math", "text": "pages 40-42", "remark": "", "date": "27/11"}
	}
	return map[string]any{}
}

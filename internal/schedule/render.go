package schedule

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// DefaultChildTemplate is the evening plan sent to a child (CLAUDE.md §7).
const DefaultChildTemplate = `🎒 {{.FirstName}}, המערכת למחר – {{hebDay .Plan.Date}} {{date .Plan.Date "02/01"}}

{{range .Plan.Lessons}}{{.Num}}. {{.Start}}–{{.End}}  {{.Subject}}{{with .Room}} · חדר {{.}}{{end}}{{with .Note}}  {{.}}{{end}}
{{end}}{{if .Plan.Homework}}
📚 שיעורי בית שניתנו היום:
{{range .Plan.Homework}}• {{.Subject}}: {{.Text}}
{{end}}{{end}}
לילה טוב 🌙`

// ChildView is the data a template renders for one child.
type ChildView struct {
	FirstName string
	Plan      Plan
}

var hebDays = [...]string{"יום ראשון", "יום שני", "יום שלישי", "יום רביעי", "יום חמישי", "יום שישי", "שבת"}

var funcs = template.FuncMap{
	"hebDay": func(t time.Time) string { return hebDays[int(t.Weekday())] },
	"date":   func(t time.Time, layout string) string { return t.Format(layout) },
}

// Render executes tmpl (or DefaultChildTemplate when empty) for a child view.
func Render(tmpl string, v ChildView) (string, error) {
	if tmpl == "" {
		tmpl = DefaultChildTemplate
	}
	t, err := template.New("plan").Funcs(funcs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

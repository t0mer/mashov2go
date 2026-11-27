package templates

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultsRenderForEveryLanguageAndAudience(t *testing.T) {
	data := map[string]any{"child": "Demo", "subject": "Math", "event": "Quiz", "value": "92", "updated": false,
		"name": "איחור", "lesson": 3, "date": "02/11", "justification": "", "text": "pages 4-5", "remark": "", "timestamp": ""}
	for _, c := range Categories {
		for _, lang := range Languages {
			for _, aud := range Audiences {
				body, ok := Default(c, lang, aud)
				if !ok {
					continue
				}
				tpl, err := Parse(body)
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", c, lang, aud, err)
				}
				if _, err := Execute(tpl, data); err != nil {
					t.Fatalf("%s/%s/%s render: %v", c, lang, aud, err)
				}
			}
		}
	}
}

func TestGradeUpdatedWording(t *testing.T) {
	out, err := Renderer{}.Render(context.Background(), "grades", "he", "parent",
		map[string]any{"child": "Demo", "subject": "Math", "event": "", "value": "85", "updated": true})
	if err != nil || !strings.Contains(out, "ציון עודכן") || !strings.Contains(out, "85") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestParseRejectsBadBodies(t *testing.T) {
	for _, bad := range []string{"", "   ", "{{.child", "{{if .x}}no end"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := Parse(strings.Repeat("x", 2001)); err == nil {
		t.Error("accepted an over-long template")
	}
}

func TestMissingFieldIsAnError(t *testing.T) {
	tpl, _ := Parse("{{.nope}}")
	if _, err := Execute(tpl, map[string]any{}); err == nil {
		t.Fatal("a missing field rendered as blank instead of failing")
	}
}

func TestTrimHelper(t *testing.T) {
	tpl, err := Parse(`{{trim 3 .text}}`)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := Execute(tpl, map[string]any{"text": "שלום עולם"})
	if out != "שלו…" {
		t.Fatalf("trim = %q", out)
	}
}

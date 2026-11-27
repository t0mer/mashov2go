package store

import (
	"context"
	"testing"
	"time"
)

func TestTemplateOverrideLifecycle(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	if _, ok, _ := s.GetTemplate(ctx, "grades", "he", "parent"); ok {
		t.Fatal("override present before any was stored")
	}
	if err := s.PutTemplate(ctx, Template{Category: "grades", Language: "he", Audience: "parent", Body: "v1"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTemplate(ctx, Template{Category: "grades", Language: "he", Audience: "parent", Body: "v2"}, now); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := s.GetTemplate(ctx, "grades", "he", "parent")
	if !ok || got.Body != "v2" {
		t.Fatalf("after replace: %+v ok=%v", got, ok)
	}
	list, _ := s.ListTemplates(ctx)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if err := s.DeleteTemplate(ctx, "grades", "he", "parent"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetTemplate(ctx, "grades", "he", "parent"); ok {
		t.Fatal("override still present after delete")
	}
}

func TestTemplateRejectsUnknownAudienceAndLanguage(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.PutTemplate(ctx, Template{Category: "grades", Language: "fr", Audience: "parent", Body: "x"}, time.Now()); err == nil {
		t.Fatal("unknown language accepted")
	}
	if err := s.PutTemplate(ctx, Template{Category: "grades", Language: "he", Audience: "everyone", Body: "x"}, time.Now()); err == nil {
		t.Fatal("unknown audience accepted")
	}
}

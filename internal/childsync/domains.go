package childsync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
)

// Source fetches a child's records (implemented by mashovsource.Source).
type Source interface {
	Grades(ctx context.Context, child family.Member, start, end time.Time) ([]mashov.Grade, error)
	Behave(ctx context.Context, child family.Member, start, end time.Time) ([]mashov.Behave, error)
	Homework(ctx context.Context, child family.Member) ([]mashov.HomeworkItem, error)
}

// GradesDomain syncs grades. Grades are a parent category by default.
func GradesDomain(src Source) Domain {
	return Domain{
		Name:     "grades",
		Category: family.CatGrades,
		Fetch: func(ctx context.Context, child family.Member, from, to time.Time) ([]any, error) {
			items, err := src.Grades(ctx, child, from, to)
			if err != nil {
				return nil, err
			}
			return anySlice(items), nil
		},
		Data: gradeData,
	}
}

// BehaveDomain syncs behaviour records. Behaviour is a parent category by default.
func BehaveDomain(src Source) Domain {
	return Domain{
		Name:     "behave",
		Category: family.CatBehave,
		Fetch: func(ctx context.Context, child family.Member, from, to time.Time) ([]any, error) {
			items, err := src.Behave(ctx, child, from, to)
			if err != nil {
				return nil, err
			}
			return anySlice(items), nil
		},
		Data: behaveData,
	}
}

// HomeworkDomain syncs homework. New homework goes to the child by default (CLAUDE.md §6.2).
func HomeworkDomain(src Source) Domain {
	return Domain{
		Name:     "homework",
		Category: family.CatHomework,
		Fetch: func(ctx context.Context, child family.Member, _, _ time.Time) ([]any, error) {
			items, err := src.Homework(ctx, child)
			if err != nil {
				return nil, err
			}
			return anySlice(items), nil
		},
		Data: homeworkData,
	}
}

func anySlice[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// shortDate turns Mashov's "2026-11-02T00:00:00" into "02/11" ("" when unreadable).
func shortDate(s string) string {
	t, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(s, "Z"))
	if err != nil {
		return ""
	}
	return t.Format("02/01")
}

// gradeData is the template data for a grade. Rate is the number when the school
// uses one; otherwise the textual grade is shown.
func gradeData(child, kind, payload string) (map[string]any, error) {
	var g mashov.Grade
	if err := json.Unmarshal([]byte(payload), &g); err != nil {
		return nil, fmt.Errorf("grade payload: %w", err)
	}
	value := g.RangeGrade
	if g.Rate > 0 {
		value = fmt.Sprint(g.Rate.Int())
	}
	return map[string]any{
		"child":   child,
		"subject": g.SubjectName,
		"event":   g.GradingEvent,
		"value":   value,
		"updated": strings.HasSuffix(kind, "updated"),
	}, nil
}

// behaveData is the template data for a behaviour record, in the school's own wording.
func behaveData(child, kind, payload string) (map[string]any, error) {
	var h mashov.Behave
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		return nil, fmt.Errorf("behave payload: %w", err)
	}
	return map[string]any{
		"child":         child,
		"name":          h.AchvaName,
		"subject":       h.Subject,
		"lesson":        h.Lesson.Int(),
		"date":          shortDate(h.LessonDate),
		"justification": h.Justification,
		"updated":       strings.HasSuffix(kind, "updated"),
	}, nil
}

// homeworkData is the template data for homework.
func homeworkData(child, kind, payload string) (map[string]any, error) {
	var h mashov.HomeworkItem
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		return nil, fmt.Errorf("homework payload: %w", err)
	}
	return map[string]any{
		"child":   child,
		"subject": h.SubjectName,
		"text":    h.Homework,
		"remark":  h.Remark,
		"date":    shortDate(h.LessonDate),
		"updated": strings.HasSuffix(kind, "updated"),
	}, nil
}

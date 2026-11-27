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
		Message: homeworkMessage,
	}
}

// homeworkMessage renders homework for the child (CLAUDE.md §7 child template).
func homeworkMessage(child, kind, payload string) (string, error) {
	var h mashov.HomeworkItem
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		return "", fmt.Errorf("homework payload: %w", err)
	}
	head := "📚 שיעורי בית חדשים"
	if strings.HasSuffix(kind, "updated") {
		head = "📚 שיעורי בית עודכנו"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s – %s\n%s", head, h.SubjectName, h.Homework)
	if h.Remark != "" {
		fmt.Fprintf(&b, "\n%s", h.Remark)
	}
	if t, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(h.LessonDate, "Z")); err == nil {
		fmt.Fprintf(&b, "\n(ניתן %s)", t.Format("02/01"))
	}
	return b.String(), nil
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
		Message: gradeMessage,
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
		Message: behaveMessage,
	}
}

func anySlice[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// gradeMessage renders a grade (CLAUDE.md §7, parent template). Rate is the number
// when the school uses one; otherwise the textual grade is shown.
func gradeMessage(child, kind, payload string) (string, error) {
	var g mashov.Grade
	if err := json.Unmarshal([]byte(payload), &g); err != nil {
		return "", fmt.Errorf("grade payload: %w", err)
	}
	head := "📊 ציון חדש"
	if strings.HasSuffix(kind, "updated") {
		head = "📊 ציון עודכן"
	}
	value := g.RangeGrade
	if g.Rate > 0 {
		value = fmt.Sprint(g.Rate.Int())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s – %s\n%s", head, child, g.SubjectName)
	if g.GradingEvent != "" {
		fmt.Fprintf(&b, " · %s", g.GradingEvent)
	}
	fmt.Fprintf(&b, "\nציון: %s", value)
	return b.String(), nil
}

// behaveMessage renders a behaviour record with the school's own wording. Codes
// are not translated here; the name is what the school recorded.
func behaveMessage(child, kind, payload string) (string, error) {
	var h mashov.Behave
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		return "", fmt.Errorf("behave payload: %w", err)
	}
	head := "⚠️ " + h.AchvaName
	if strings.HasSuffix(kind, "updated") {
		head = "⚠️ עודכן: " + h.AchvaName
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s – %s\n%s · שיעור %d", head, child, h.Subject, h.Lesson.Int())
	if t, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(h.LessonDate, "Z")); err == nil {
		fmt.Fprintf(&b, " · %s", t.Format("02/01"))
	}
	if h.Justification != "" {
		fmt.Fprintf(&b, "\n%s", h.Justification)
	}
	return b.String(), nil
}

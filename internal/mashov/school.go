package mashov

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Lesson is one weekly timetable slot for a child (students/{sg}/timetable).
type Lesson struct {
	TimeTable    TimeTableSlot `json:"timeTable"`
	GroupDetails GroupDetails  `json:"groupDetails"`
}

// TimeTableSlot is the scheduling part of a Lesson. Day is Sunday=1 … Saturday=7.
type TimeTableSlot struct {
	Day     int    `json:"day"`
	GroupID int    `json:"groupId"`
	Lesson  int    `json:"lesson"`
	RoomNum string `json:"roomNum"`
	Weeks   int    `json:"weeks"`
}

// GroupDetails describes the class group behind a slot.
type GroupDetails struct {
	GroupID     int    `json:"groupId"`
	GroupName   string `json:"groupName"`
	SubjectName string `json:"subjectName"`
}

// TimetableChanges is GET students/{sg}/timetable/changes.
type TimetableChanges struct {
	IsEnabled     bool         `json:"isEnabled"`
	Changes       []ChangeItem `json:"changes"`
	Cancellations []ChangeItem `json:"cancellations"`
}

// ChangeItem is one substitution/cancellation/move. Only fields confirmed in
// Phase 1 are typed; the rest are kept raw until a live example is captured.
type ChangeItem struct {
	GroupID    int    `json:"groupId"`
	LessonDate string `json:"lessonDate"`
	Lesson     int    `json:"lesson"`
	Action     int    `json:"action"`
}

// Action values from CLAUDE.md §2.
const (
	ActionSubstitute = 0
	ActionFree       = 1
	ActionCancel     = 2
	ActionMove       = 3
	ActionAddition   = 4
	ActionExtra      = 5
	ActionOnline     = 8
)

// Bell is one lesson time slot (GET bells).
type Bell struct {
	LessonNumber int    `json:"lessonNumber"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime"`
}

// Holiday is one holiday range (GET holidays).
type Holiday struct {
	ID        int    `json:"id"`
	Name      string `json:"hollyDayName"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// HomeworkItem is one homework entry (students/{sg}/homework).
type HomeworkItem struct {
	GroupID     int    `json:"groupId"`
	Homework    string `json:"homework"`
	Lesson      int    `json:"lesson"`
	LessonDate  string `json:"lessonDate"`
	LessonID    int    `json:"lessonId"`
	Remark      string `json:"remark"`
	SubjectName string `json:"subjectName"`
}

// Timetable returns the child's weekly timetable.
func (c *Client) Timetable(ctx context.Context, sg string) ([]Lesson, error) {
	var out []Lesson
	return out, c.getJSON(ctx, "students/"+sg+"/timetable", nil, &out)
}

// TimetableChangesFor returns changes and cancellations in [start, end).
func (c *Client) TimetableChangesFor(ctx context.Context, sg string, start, end time.Time) (*TimetableChanges, error) {
	var out TimetableChanges
	q := rangeQuery(start, end)
	return &out, c.getJSON(ctx, "students/"+sg+"/timetable/changes", q, &out)
}

// Bells returns the lesson time table.
func (c *Client) Bells(ctx context.Context) ([]Bell, error) {
	var out []Bell
	return out, c.getJSON(ctx, "bells", nil, &out)
}

// Holidays returns holiday ranges.
func (c *Client) Holidays(ctx context.Context) ([]Holiday, error) {
	var out []Holiday
	return out, c.getJSON(ctx, "holidays", nil, &out)
}

// Homework returns the child's homework list.
func (c *Client) Homework(ctx context.Context, sg string) ([]HomeworkItem, error) {
	var out []HomeworkItem
	return out, c.getJSON(ctx, "students/"+sg+"/homework", nil, &out)
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	b, err := c.get(ctx, path, q)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

func rangeQuery(start, end time.Time) url.Values {
	return url.Values{
		"start": {start.UTC().Format(time.RFC3339)},
		"end":   {end.UTC().Format(time.RFC3339)},
	}
}

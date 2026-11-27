package mashov

import (
	"context"
	"time"
)

// Behave is one item of students/{sg}/behave. Field names are the ones the live
// account returns. AchvaName is the school's own wording and is shown as is;
// AchvaCode and EventCode are numbers whose meaning is not mapped here.
type Behave struct {
	AchvaAval       int    `json:"achvaAval"`
	AchvaCode       int    `json:"achvaCode"`
	AchvaName       string `json:"achvaName"`
	EventCode       int    `json:"eventCode"`
	GroupID         int    `json:"groupId"`
	Justification   string `json:"justification"`
	JustificationID int    `json:"justificationId"`
	Justified       int    `json:"justified"`
	Lesson          int    `json:"lesson"`
	LessonDate      string `json:"lessonDate"`
	LessonID        int    `json:"lessonId"`
	LessonReporter  string `json:"lessonReporter"`
	LessonType      int    `json:"lessonType"`
	Reporter        string `json:"reporter"`
	ReporterGUID    string `json:"reporterGuid"`
	StudentGUID     string `json:"studentGuid"`
	Subject         string `json:"subject"`
	Timestamp       string `json:"timestamp"`
}

// Behave returns the child's behaviour records between start and end (UTC).
func (c *Client) Behave(ctx context.Context, sg string, start, end time.Time) ([]Behave, error) {
	var out []Behave
	return out, c.getJSON(ctx, "students/"+sg+"/behave", rangeQuery(start, end), &out)
}

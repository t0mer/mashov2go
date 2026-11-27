package mashov

import (
	"context"
	"time"
)

// Grade is one item of students/{sg}/grades. Field names are the ones the live
// account returns (see the shape notes in memory). Rate is the numeric grade and
// RangeGrade the textual one; which is set depends on the grading scheme.
type Grade struct {
	ID             Number `json:"id"`
	GradingEventID Number `json:"gradingEventId"`
	GradingEvent   string `json:"gradingEvent"`
	GradeType      string `json:"gradeType"`
	GradeTypeID    Number `json:"gradeTypeId"`
	GradingPeriod  Number `json:"gradingPeriod"`
	GradeRate      Number `json:"gradeRate"`
	Rate           Number `json:"rate"`
	RangeGrade     string `json:"rangeGrade"`
	SubjectName    string `json:"subjectName"`
	GroupID        Number `json:"groupId"`
	GroupName      string `json:"groupName"`
	TeacherName    string `json:"teacherName"`
	EventDate      string `json:"eventDate"`
	Timestamp      string `json:"timestamp"`
	StudentGUID    string `json:"studentGuid"`
	Year           Number `json:"year"`
}

// Grades returns the child's grades between start and end (UTC).
func (c *Client) Grades(ctx context.Context, sg string, start, end time.Time) ([]Grade, error) {
	var out []Grade
	return out, c.getJSON(ctx, "students/"+sg+"/grades", rangeQuery(start, end), &out)
}

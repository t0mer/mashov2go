package mashov

import "context"

// Notification is one item of user/notifications. EventType selects the domain
// to refresh (CLAUDE.md §5.2); EventUserID is the student the item belongs to.
// EventData is a JSON string whose shape depends on the type; it is kept raw.
type Notification struct {
	ID          Number `json:"id"`
	EventType   Number `json:"eventType"`
	EventText   string `json:"eventText"`
	EventTime   string `json:"eventTime"`
	EventUserID string `json:"eventUserId"`
	EventKey    string `json:"eventKey"`
	EventKey2   string `json:"eventKey2"`
	ReadTime    string `json:"readTime"`
}

// Notifications returns the most recent notifications (the server returns the
// latest 50 when no paging is given). Reading them does not mark them read.
func (c *Client) Notifications(ctx context.Context) ([]Notification, error) {
	var out []Notification
	return out, c.getJSON(ctx, "user/notifications", nil, &out)
}

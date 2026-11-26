package mashov

import (
	"context"
	"encoding/json"
	"fmt"
)

// School is one entry of GET schools.
type School struct {
	Semel int    `json:"semel"`
	Name  string `json:"name"`
	Years []int  `json:"years"`
}

// Schools lists every school Mashov knows about (unauthenticated endpoint).
// Callers should cache the result for 24 h (CLAUDE.md §3.1).
func (c *Client) Schools(ctx context.Context) ([]School, error) {
	b, err := c.get(ctx, "schools", nil)
	if err != nil {
		return nil, err
	}
	var out []School
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decoding schools: %w", err)
	}
	return out, nil
}

// DefaultYear returns the year to pre-select for a school: the newest listed year.
func (s School) DefaultYear() int {
	best := 0
	for _, y := range s.Years {
		if y > best {
			best = y
		}
	}
	return best
}

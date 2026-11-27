package mashov

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Number is a numeric field that Mashov sometimes sends as a decimal ("0.0") and
// sometimes as an integer. It also accepts a quoted number. Decoding into int
// fails on the decimal form, which stopped the live sync in the first run.
type Number float64

// UnmarshalJSON accepts a JSON number or a quoted number.
func (n *Number) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "null" || s == "" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("not a number: %w", err)
	}
	*n = Number(f)
	return nil
}

// MarshalJSON writes a whole number as an integer, so stored payloads stay stable.
func (n Number) MarshalJSON() ([]byte, error) {
	if float64(n) == float64(int64(n)) {
		return json.Marshal(int64(n))
	}
	return json.Marshal(float64(n))
}

// Int returns the value as an integer (fractions are truncated).
func (n Number) Int() int { return int(n) }

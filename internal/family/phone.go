// Package family holds the members (parents and children), their recipients and
// their channels (CLAUDE.md §6).
package family

import (
	"errors"
	"regexp"
	"strings"
)

var (
	nonDigits = regexp.MustCompile(`\D`)
	e164      = regexp.MustCompile(`^[1-9]\d{7,14}$`)
)

// NormalizePhone returns the E.164 digits (no '+') for a phone number.
// Israeli numbers written as 05X… become 9725X… (CLAUDE.md §6.1).
func NormalizePhone(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	digits := nonDigits.ReplaceAllString(s, "")
	if strings.HasPrefix(digits, "00") {
		digits = digits[2:]
	}
	if strings.HasPrefix(digits, "05") && len(digits) == 10 {
		digits = "972" + digits[1:]
	}
	if !e164.MatchString(digits) {
		return "", errors.New("phone must be a valid international number, e.g. 0501234567 or 972501234567")
	}
	return digits, nil
}

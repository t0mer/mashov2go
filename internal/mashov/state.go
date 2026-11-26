package mashov

import "strings"

// State is the per-account authentication state shown in the UI (CLAUDE.md §3.2).
type State string

const (
	StateOK          State = "ok"
	StateAuthFailed  State = "auth_failed"
	StateNeedsManual State = "needs_manual"
	StateBlocked     State = "blocked"
)

// MapLoginReason maps a failed login's HTTP status and Mashov `Reason` header
// to a State. Unknown reasons map to auth_failed (CLAUDE.md §3.2 table).
func MapLoginReason(_ int, reason string) State {
	switch reason {
	case "UserSuspended", "ChangePass", "CreateUsername", "SetUsernameAndPass":
		return StateNeedsManual
	case "NoParentsAccess":
		return StateBlocked
	}
	// Captcha challenges have no stable Reason name; match by substring.
	if strings.Contains(strings.ToLower(reason), "captcha") {
		return StateNeedsManual
	}
	return StateAuthFailed
}

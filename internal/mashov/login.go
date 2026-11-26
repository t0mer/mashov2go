package mashov

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// DeviceFields are the app/device identity sent with login and loginDevice.
type DeviceFields struct {
	AppName            string `json:"appName,omitempty"`
	APIVersion         string `json:"apiVersion,omitempty"`
	AppVersion         string `json:"appVersion,omitempty"`
	AppBuild           string `json:"appBuild,omitempty"`
	DeviceUUID         string `json:"deviceUuid,omitempty"`
	DevicePlatform     string `json:"devicePlatform,omitempty"`
	DeviceManufacturer string `json:"deviceManufacturer,omitempty"`
	DeviceModel        string `json:"deviceModel,omitempty"`
	DeviceVersion      string `json:"deviceVersion,omitempty"`
}

// LoginRequest is the body of POST login (CLAUDE.md §3.2).
type LoginRequest struct {
	Semel           int    `json:"semel"`
	Year            int    `json:"year"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	IsPrivateDevice bool   `json:"isPrivateDevice"`
	IsBiometric     bool   `json:"IsBiometric"`
	DeviceFields
}

// Credential is the `credential` object of a login response. Types that are not
// yet confirmed against live traffic are kept raw.
type Credential struct {
	Semel    int             `json:"semel"`
	Year     int             `json:"year"`
	UserID   json.RawMessage `json:"userId,omitempty"`
	UserType json.RawMessage `json:"userType,omitempty"`
	IDNumber json.RawMessage `json:"idNumber,omitempty"`
}

// Child is one entry of accessToken.children / externChildren.
type Child struct {
	ChildGUID   string          `json:"childGuid"`
	PrivateName string          `json:"privateName"`
	FamilyName  string          `json:"familyName"`
	ClassCode   json.RawMessage `json:"classCode,omitempty"`
	ClassNum    json.RawMessage `json:"classNum,omitempty"`
}

// AccessToken is the `accessToken` object of a login response.
type AccessToken struct {
	DisplayName    string          `json:"displayName"`
	Children       []Child         `json:"children"`
	ExternChildren []Child         `json:"externChildren"`
	SchoolOptions  json.RawMessage `json:"schoolOptions,omitempty"`
}

// LoginResponse is a successful login. DevicePass, when present, must be
// stored encrypted by the caller; this package never persists it.
type LoginResponse struct {
	Credential  Credential  `json:"credential"`
	AccessToken AccessToken `json:"accessToken"`
	DevicePass  string      `json:"-"`
}

// LoginError is a failed login with the state the UI should show.
type LoginError struct {
	Status int
	Reason string
	State  State
}

func (e *LoginError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("mashov login failed: HTTP %d (%s)", e.Status, e.State)
	}
	return fmt.Sprintf("mashov login failed: HTTP %d reason %s (%s)", e.Status, e.Reason, e.State)
}

// minDevicePassLen is the length below which a devicePass is treated as revoked/absent.
const minDevicePassLen = 20

// Login performs a password (or OTP-as-password) login. It is never retried
// automatically (CLAUDE.md §0.4, §3.5). Pass devicePass if one is held.
func (c *Client) Login(ctx context.Context, req LoginRequest, devicePass string) (*LoginResponse, error) {
	return c.login(ctx, "login", req, devicePass)
}

// LoginDevice re-authenticates with a stored devicePass (CLAUDE.md §3.3).
// The body carries only the app/device fields; the devicePass travels in its header.
func (c *Client) LoginDevice(ctx context.Context, fields DeviceFields, devicePass string) (*LoginResponse, error) {
	if devicePass == "" {
		return nil, errors.New("loginDevice requires a devicePass")
	}
	body := struct {
		DeviceFields
	}{fields}
	return c.login(ctx, "loginDevice", body, devicePass)
}

func (c *Client) login(ctx context.Context, path string, body any, devicePass string) (*LoginResponse, error) {
	var extra http.Header
	if devicePass != "" {
		extra = http.Header{"DevicePass": []string{devicePass}}
	}
	r, err := c.send(ctx, http.MethodPost, path, nil, body, extra)
	if err != nil {
		return nil, err
	}
	if r.status != http.StatusOK {
		reason := r.header.Get("Reason")
		return nil, &LoginError{Status: r.status, Reason: reason, State: MapLoginReason(r.status, reason)}
	}
	var out LoginResponse
	if err := json.Unmarshal(r.body, &out); err != nil {
		return nil, fmt.Errorf("decoding %s response: %w", path, err)
	}
	if tok := r.header.Get("X-Csrf-Token"); tok != "" {
		c.setCSRF(tok)
	}
	if dp := r.header.Get("DevicePass"); len(dp) >= minDevicePassLen {
		out.DevicePass = dp
	}
	return &out, nil
}

// IsActive probes GET session/isActive. A nil error means the session is live.
func (c *Client) IsActive(ctx context.Context) error {
	_, err := c.getWith(ctx, "session/isActive", nil, false)
	return err
}

// Logout ends the server-side session (GET logout). Used when an account is removed.
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.getWith(ctx, "logout", nil, false)
	return err
}

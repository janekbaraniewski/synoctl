package dsm

import (
	"context"
	"net/url"
)

// LoginRequest carries credentials and optional 2FA inputs.
type LoginRequest struct {
	Account     string
	Password    string
	OTP         string // 6-digit code from authenticator app
	DeviceID    string // returned from a prior login with DeviceName set
	DeviceName  string // when set, request a device token to skip OTP next time
	SessionName string // logical session label; defaults to "DSM"
}

// LoginResponse mirrors the SYNO.API.Auth/login data envelope.
type LoginResponse struct {
	SID       string `json:"sid"`
	DID       string `json:"did,omitempty"`       // device token, when requested
	DeviceID  string `json:"device_id,omitempty"` // alternative name on some firmware
	SynoToken string `json:"synotoken,omitempty"` // CSRF token
	IsPortal  bool   `json:"is_portal,omitempty"`
	Account   string `json:"account,omitempty"`
}

// Login authenticates the client and stores the resulting SID + CSRF
// token. Subsequent Call()s are then authorised automatically.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	session := req.SessionName
	if session == "" {
		session = "DSM"
	}
	params := url.Values{}
	params.Set("account", req.Account)
	params.Set("passwd", req.Password)
	params.Set("session", session)
	params.Set("format", "sid")
	params.Set("enable_syno_token", "yes")
	if req.OTP != "" {
		params.Set("otp_code", req.OTP)
	}
	if req.DeviceID != "" {
		params.Set("device_id", req.DeviceID)
	}
	if req.DeviceName != "" {
		params.Set("enable_device_token", "yes")
		params.Set("device_name", req.DeviceName)
	}

	var resp LoginResponse
	// DSM 7 uses Auth v6/v7; v6 covers both DSM 6.2 and 7.x.
	if err := c.Call(ctx, "SYNO.API.Auth", 6, "login", params, &resp); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.sid = resp.SID
	if resp.SynoToken != "" {
		c.synoToken = resp.SynoToken
	}
	did := resp.DID
	if did == "" {
		did = resp.DeviceID
	}
	if did != "" {
		c.deviceID = did
	}
	c.mu.Unlock()
	return &resp, nil
}

// DeviceID returns the device token issued at login (or supplied to it).
func (c *Client) DeviceID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deviceID
}

// Logout invalidates the current SID server-side and clears local state.
func (c *Client) Logout(ctx context.Context) error {
	if !c.Authenticated() {
		return nil
	}
	params := url.Values{}
	params.Set("session", "DSM")
	err := c.Call(ctx, "SYNO.API.Auth", 1, "logout", params, nil)
	c.mu.Lock()
	c.sid = ""
	c.synoToken = ""
	c.mu.Unlock()
	return err
}

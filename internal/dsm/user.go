package dsm

import (
	"context"
	"net/url"
)

// User is a DSM local user account.
type User struct {
	Name                string   `json:"name"`
	UID                 int      `json:"uid"`
	Description         string   `json:"description"`
	Email               string   `json:"email"`
	Expired             string   `json:"expired"` // "now", "normal", or a date
	Groups              []string `json:"groups,omitempty"`
	PasswordNeverExpire bool     `json:"password_never_expire,omitempty"`
}

// Users returns all local users. additional may include "email", "description",
// "expired", "cannot_chg_passwd", "password_last_change", "user_2step_status".
func (c *Client) Users(ctx context.Context) ([]User, error) {
	params := url.Values{}
	params.Set("type", "local")
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("additional", `["email","description","expired","cannot_chg_passwd","password_last_change","user_2step_status"]`)
	var resp struct {
		Users  []User `json:"users"`
		Offset int    `json:"offset"`
		Total  int    `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.Core.User", 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Users, nil
}

// Group is a DSM local group.
type Group struct {
	Name        string `json:"name"`
	GID         int    `json:"gid"`
	Description string `json:"description"`
}

// Groups returns all local groups. DSM 7 exposes this under
// SYNO.Core.User.Group (not SYNO.Core.Group, which doesn't exist).
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	params := url.Values{}
	params.Set("type", "local")
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("additional", `["description"]`)
	var resp struct {
		Groups []Group `json:"groups"`
		Offset int     `json:"offset"`
		Total  int     `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.Core.User.Group", 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Groups, nil
}

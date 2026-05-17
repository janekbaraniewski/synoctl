package dsm

import (
	"context"
	"encoding/json"
	"net/url"
)

// Package describes an installed DSM application package. Fields
// outside id/name/version/timestamp arrive nested under `additional`;
// UnmarshalJSON flattens them.
type Package struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Timestamp    int64  `json:"timestamp,omitempty"`
	Status       string `json:"status,omitempty"` // running / stop / broken
	Maintainer   string `json:"maintainer,omitempty"`
	Description  string `json:"description,omitempty"`
	Beta         bool   `json:"beta,omitempty"`
	CtlUninstall bool   `json:"ctl_uninstall,omitempty"`
}

func (p *Package) UnmarshalJSON(b []byte) error {
	type alias Package
	var raw struct {
		alias
		Additional struct {
			Status       string `json:"status"`
			StatusOrigin string `json:"status_origin"`
			Maintainer   string `json:"maintainer"`
			Description  string `json:"description"`
			Beta         bool   `json:"beta"`
			CtlUninstall bool   `json:"ctl_uninstall"`
		} `json:"additional"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*p = Package(raw.alias)
	if p.Status == "" {
		p.Status = raw.Additional.Status
	}
	if p.Maintainer == "" {
		p.Maintainer = raw.Additional.Maintainer
	}
	if p.Description == "" {
		p.Description = raw.Additional.Description
	}
	if !p.Beta {
		p.Beta = raw.Additional.Beta
	}
	if !p.CtlUninstall {
		p.CtlUninstall = raw.Additional.CtlUninstall
	}
	return nil
}

// Packages returns installed packages with status + metadata.
func (c *Client) Packages(ctx context.Context) ([]Package, error) {
	params := url.Values{}
	params.Set("additional", `["status","ctl_uninstall","beta","maintainer","description"]`)
	var resp struct {
		Packages []Package `json:"packages"`
		Total    int       `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.Core.Package", 2, "list", params, &resp); err == nil {
		return resp.Packages, nil
	}
	// Older firmware: drop the additional set and retry on v1.
	if err := c.Call(ctx, "SYNO.Core.Package", 1, "list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Packages, nil
}

// PackageUninstall removes an installed package.
func (c *Client) PackageUninstall(ctx context.Context, id string) error {
	params := url.Values{}
	params.Set("id", id)
	return c.Call(ctx, "SYNO.Core.Package.Uninstallation", 1, "uninstall", params, nil)
}

// PackageControl starts, stops, or restarts a package by id.
func (c *Client) PackageControl(ctx context.Context, id, action string) error {
	params := url.Values{}
	params.Set("id", id)
	switch action {
	case "start":
		return c.Call(ctx, "SYNO.Core.Package.Control", 1, "start", params, nil)
	case "stop":
		return c.Call(ctx, "SYNO.Core.Package.Control", 1, "stop", params, nil)
	case "restart":
		if err := c.Call(ctx, "SYNO.Core.Package.Control", 1, "stop", params, nil); err != nil {
			return err
		}
		return c.Call(ctx, "SYNO.Core.Package.Control", 1, "start", params, nil)
	}
	return nil
}

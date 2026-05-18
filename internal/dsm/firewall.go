package dsm

import (
	"context"
	"net/url"
)

// FirewallConf mirrors SYNO.Core.Network.Firewall.Conf "get" — the
// global firewall state (enabled / disabled, currently bound profile,
// notification preferences).
type FirewallConf struct {
	Enable          flexBool `json:"enable"`
	ProfileName     string   `json:"profile_name,omitempty"`
	ProfileID       int      `json:"profile_id,omitempty"`
	NotifyDeny      flexBool `json:"notify_deny,omitempty"`
	LogDeny         flexBool `json:"log_deny,omitempty"`
	GeoDBVersion    string   `json:"geo_db_version,omitempty"`
	DefaultPolicy   string   `json:"default_policy,omitempty"` // "allow" / "deny"
	AdapterStatuses []struct {
		Adapter string   `json:"adapter"`
		Enabled flexBool `json:"enable"`
	} `json:"adapters,omitempty"`
}

// FirewallStatus returns the global firewall configuration via
// SYNO.Core.Network.Firewall.Conf "get" v1. Returns nil (and nil error)
// when the API is not advertised.
func (c *Client) FirewallStatus(ctx context.Context) (*FirewallConf, error) {
	const api = "SYNO.Core.Network.Firewall.Conf"
	if !c.Supports(api) {
		return nil, nil
	}
	var out FirewallConf
	if err := c.Call(ctx, api, 1, "get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FirewallProfile is one entry from SYNO.Core.Network.Firewall.Profile.list
// — a named ruleset that can be bound to one or more network adapters.
type FirewallProfile struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"desc,omitempty"`
	RuleCount   int      `json:"rule_count,omitempty"`
	InUse       flexBool `json:"in_use,omitempty"`
	IsDefault   flexBool `json:"is_default,omitempty"`
}

// FirewallProfiles lists firewall profiles via
// SYNO.Core.Network.Firewall.Profile "list" v1. Returns an empty slice
// (and nil error) when the API is not advertised.
func (c *Client) FirewallProfiles(ctx context.Context) ([]FirewallProfile, error) {
	const api = "SYNO.Core.Network.Firewall.Profile"
	if !c.Supports(api) {
		return []FirewallProfile{}, nil
	}
	var resp struct {
		Profiles []FirewallProfile `json:"profiles"`
		Total    int               `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Profiles, nil
}

// FirewallRule is one entry from SYNO.Core.Network.Firewall.Rules.list —
// a single ordered rule within a profile. ip_type / src_type values
// follow DSM: "all", "ip", "range", "subnet". port_dst is a free-form
// string ("80", "1024-65535", "tcp/22").
type FirewallRule struct {
	RuleID    int      `json:"rule_id"`
	ProfileID int      `json:"profile_id,omitempty"`
	Order     int      `json:"order,omitempty"`
	Enable    flexBool `json:"enable,omitempty"`
	Policy    string   `json:"policy,omitempty"` // "accept" / "drop"
	Protocol  string   `json:"protocol,omitempty"`
	PortDst   string   `json:"port_dst,omitempty"`
	SrcType   string   `json:"src_type,omitempty"`
	SrcIP     string   `json:"src_ip,omitempty"`
	SrcSubnet string   `json:"src_subnet,omitempty"`
	SrcGeo    []string `json:"src_geo,omitempty"`
	Adapter   string   `json:"adapter,omitempty"`
	Comment   string   `json:"comment,omitempty"`
}

// FirewallRules lists the ordered firewall rules for the given profile
// via SYNO.Core.Network.Firewall.Rules "list" v1. Pass an empty string
// for the active profile. Returns an empty slice (and nil error) when
// the API is not advertised.
func (c *Client) FirewallRules(ctx context.Context, profile string) ([]FirewallRule, error) {
	const api = "SYNO.Core.Network.Firewall.Rules"
	if !c.Supports(api) {
		return []FirewallRule{}, nil
	}
	params := url.Values{}
	if profile != "" {
		params.Set("profile", profile)
	}
	var resp struct {
		Rules []FirewallRule `json:"rules"`
		Total int            `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

package dsm

import (
	"context"
	"net/url"
)

// Service is a system-level service (SMB, AFP, SSH, …) reported by DSM.
//
// Field names match SYNO.Core.Service v3 get: `service_id` is the stable
// identifier, `enable_status` is "enabled" / "disabled" / "static"
// ("static" means the service runs unconditionally and can't be toggled).
type Service struct {
	ID                    string `json:"service_id"`
	DisplayNameSectionKey string `json:"display_name_section_key"`
	EnableStatus          string `json:"enable_status"`
}

// DisplayName returns a human label for the service. DSM ships the real
// names in i18n bundles only, so we map the well-known ids to friendly
// strings and fall back to the id (or section key) for anything else.
func (s Service) DisplayName() string {
	if name, ok := serviceFriendlyNames[s.ID]; ok {
		return name
	}
	return s.ID
}

// Enabled reports whether the service is currently enabled.
func (s Service) Enabled() bool { return s.EnableStatus == "enabled" }

// Toggleable reports whether the user can flip the service. "static"
// services run unconditionally.
func (s Service) Toggleable() bool { return s.EnableStatus != "static" }

var serviceFriendlyNames = map[string]string{
	"atalk":                            "AppleTalk (AFP)",
	"bonjour":                          "Bonjour mDNS",
	"cupsd":                            "CUPS print daemon",
	"ftp-pure":                         "FTP",
	"ftp-ssl":                          "FTP over SSL",
	"nfs-server":                       "NFS",
	"ntpd":                             "NTP",
	"pkg-iscsi":                        "iSCSI",
	"pkg-synosamba-smbd":               "SMB / CIFS",
	"pkg-synosamba-wstransfer-genconf": "WS-Discovery",
	"rsync":                            "Rsync",
	"sshd":                             "SSH",
	"telnetd":                          "Telnet",
	"snmpd":                            "SNMP",
	"webstation":                       "Web Station",
	"upnp":                             "UPnP / DLNA",
}

// Services returns the system service list via SYNO.Core.Service v3 get.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	var resp struct {
		Service []Service `json:"service"`
	}
	if err := c.Call(ctx, "SYNO.Core.Service", 3, "get", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Service, nil
}

// ServiceControl issues a control action against a service.
// action ∈ {"start","stop","restart","set_enable","set_disable"}.
func (c *Client) ServiceControl(ctx context.Context, id, action string) error {
	params := url.Values{}
	switch action {
	case "start":
		params.Set("service_id", id)
		return c.Call(ctx, "SYNO.Core.Service", 3, "start", params, nil)
	case "stop":
		params.Set("service_id", id)
		return c.Call(ctx, "SYNO.Core.Service", 3, "stop", params, nil)
	case "restart":
		params.Set("service_id", id)
		if err := c.Call(ctx, "SYNO.Core.Service", 3, "stop", params, nil); err != nil {
			return err
		}
		return c.Call(ctx, "SYNO.Core.Service", 3, "start", params, nil)
	case "enable":
		params.Set("service", id)
		return c.Call(ctx, "SYNO.Core.Service", 3, "set_enable", params, nil)
	case "disable":
		params.Set("service", id)
		return c.Call(ctx, "SYNO.Core.Service", 3, "set_disable", params, nil)
	}
	return nil
}

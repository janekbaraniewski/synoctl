package dsm

import (
	"context"
)

// NetworkInterface is a single NIC reported by DSM. DSM 7 returns a thin
// set of fields by default and ignores `additional` requests on legacy
// boxes, so we model only what actually arrives.
type NetworkInterface struct {
	Name    string `json:"name,omitempty"`
	IFName  string `json:"ifname"` // eth0, bond0, pppoe
	Type    string `json:"type"`   // lan, pppoe, bond
	IP      string `json:"ip,omitempty"`
	Mask    string `json:"mask,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	MAC     string `json:"mac,omitempty"`
	Speed   int    `json:"speed"`  // Mbit/s
	Status  string `json:"status"` // connected | disconnected
	UseDHCP bool   `json:"use_dhcp,omitempty"`
	MTU     int    `json:"mtu,omitempty"`
}

// NetworkInterfaces lists network interfaces.
func (c *Client) NetworkInterfaces(ctx context.Context) ([]NetworkInterface, error) {
	var out []NetworkInterface
	if err := c.Call(ctx, "SYNO.Core.Network.Interface", 1, "list", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

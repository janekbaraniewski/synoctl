package dsm

import (
	"context"
	"net/url"
)

// VirtualMachine is one row from SYNO.Virtualization.Guest.list — a
// Virtual Machine Manager (VMM) guest. status takes the standard libvirt
// life-cycle vocabulary: running / paused / shutoff / saved / crashed.
// host is only meaningful when VMM is running as a Synology High
// Availability (SHA) cluster — for a single-node install it's blank.
// Boolean flags arrive as 0/1 on some firmware builds; flexBool absorbs
// the variation.
type VirtualMachine struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	VMID        string   `json:"vm_id,omitempty"`
	Status      string   `json:"status,omitempty"` // running / paused / shutoff / saved / crashed
	Host        string   `json:"host,omitempty"`   // cluster node when SHA
	VCPUNum     int      `json:"vcpu_num,omitempty"`
	VRAMSize    int64    `json:"vram_size,omitempty"` // MB
	Description string   `json:"description,omitempty"`
	AutoRun     flexBool `json:"auto_run,omitempty"`
	EnableHA    flexBool `json:"enable_ha,omitempty"`
}

// VirtualMachines lists VMM guests via SYNO.Virtualization.Guest "list" v1.
// Returns an empty slice (and nil error) when the device does not
// advertise SYNO.Virtualization.Guest — Virtual Machine Manager is an
// optional package and may not be installed.
func (c *Client) VirtualMachines(ctx context.Context) ([]VirtualMachine, error) {
	const api = "SYNO.Virtualization.Guest"
	if !c.Supports(api) {
		return []VirtualMachine{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Guests []VirtualMachine `json:"guests"`
		Total  int              `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Guests, nil
}

// VMHost is one entry from SYNO.Virtualization.Host.list — a
// virtualization host. On a single-box install this is just the NAS
// itself; on an SHA cluster there's one entry per node. cpu_usage and
// ram_used are point-in-time samples.
type VMHost struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	HostIP   string   `json:"host_ip,omitempty"`
	VMCount  int      `json:"vm_count,omitempty"`
	CPUUsage float64  `json:"cpu_usage,omitempty"` // percent
	RAMTotal int64    `json:"ram_total,omitempty"` // MB
	RAMUsed  int64    `json:"ram_used,omitempty"`  // MB
	Running  flexBool `json:"running,omitempty"`
}

// VMHosts lists VMM hosts via SYNO.Virtualization.Host "list" v1.
// Returns an empty slice (and nil error) when the API is not advertised.
func (c *Client) VMHosts(ctx context.Context) ([]VMHost, error) {
	const api = "SYNO.Virtualization.Host"
	if !c.Supports(api) {
		return []VMHost{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Hosts []VMHost `json:"hosts"`
		Total int      `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Hosts, nil
}

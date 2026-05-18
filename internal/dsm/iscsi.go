package dsm

import (
	"context"
	"net/url"
)

// ISCSITarget is one entry from SYNO.Core.ISCSI.Target.list — an iSCSI
// target. The IQN is the canonical "iqn.2000-01.com.synology:…" string;
// naa_id is the SCSI NAA identifier some initiators (e.g. VMware) prefer
// to address LUNs by. auth distinguishes the three DSM auth modes: none,
// CHAP (initiator → target), and mutual (both directions).
type ISCSITarget struct {
	TargetID        int      `json:"target_id"`
	Name            string   `json:"name"`
	IQN             string   `json:"iqn,omitempty"`
	Enabled         flexBool `json:"enabled,omitempty"`
	ConnectionCount int      `json:"connection_count,omitempty"`
	Auth            string   `json:"auth,omitempty"` // none / chap / mutual
	NAAID           string   `json:"naa_id,omitempty"`
}

// ISCSITargets lists iSCSI targets via SYNO.Core.ISCSI.Target "list" v1.
// Returns an empty slice (and nil error) when the API is not advertised
// — the iSCSI / SAN Manager package is optional.
func (c *Client) ISCSITargets(ctx context.Context) ([]ISCSITarget, error) {
	const api = "SYNO.Core.ISCSI.Target"
	if !c.Supports(api) {
		return []ISCSITarget{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Targets []ISCSITarget `json:"targets"`
		Total   int           `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Targets, nil
}

// ISCSILUN is one entry from SYNO.Core.ISCSI.LUN.list — an iSCSI LUN.
// type is DSM's storage-backend kind ("file" for thin-provisioned file
// LUNs, "block" for block-level LUNs, "pool-based" for newer
// volume/pool-backed LUNs on Btrfs). device_path is the canonical
// /dev/… node used by the iSCSI target driver.
type ISCSILUN struct {
	LUNID         int    `json:"lun_id"`
	Name          string `json:"name"`
	Size          int64  `json:"size,omitempty"` // bytes
	MappedTargets []int  `json:"mapped_targets,omitempty"`
	Type          string `json:"type,omitempty"` // file / block / pool-based
	DevicePath    string `json:"device_path,omitempty"`
}

// ISCSILUNs lists iSCSI LUNs via SYNO.Core.ISCSI.LUN "list" v1. Returns
// an empty slice (and nil error) when the API is not advertised.
func (c *Client) ISCSILUNs(ctx context.Context) ([]ISCSILUN, error) {
	const api = "SYNO.Core.ISCSI.LUN"
	if !c.Supports(api) {
		return []ISCSILUN{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		LUNs  []ISCSILUN `json:"luns"`
		Total int        `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.LUNs, nil
}

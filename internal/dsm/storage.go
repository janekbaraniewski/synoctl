package dsm

import (
	"context"
	"encoding/json"
)

// Storage is the aggregate response from SYNO.Storage.CGI.Storage.load_info.
// That endpoint returns volumes/pools/disks/ports/env in a single payload,
// and it's the one DSM 7 actually keeps alive on legacy boxes like the
// DS220j (the newer SYNO.Core.Storage.* endpoints rejected our params
// with code 101 on this firmware).
type Storage struct {
	Volumes      []Volume      `json:"volumes"`
	StoragePools []StoragePool `json:"storagePools"`
	Disks        []Disk        `json:"disks"`
	// Raw captures the full payload so the detail view can pretty-print
	// any field we haven't modelled explicitly.
	Raw json.RawMessage `json:"-"`
}

// Volume is a logical filesystem mounted on a pool.
//
// Field names match the legacy CGI shape: `raidType` is camelCase here
// (not snake_case), and the canonical id is `volume_1`/`volume_2`/… —
// the user-visible "/volume1" path lives under VolPath.
type Volume struct {
	ID         string `json:"id"`          // "volume_1"
	VolPath    string `json:"vol_path"`    // "/volume1"
	Container  string `json:"container"`   // "internal"
	DeviceType string `json:"device_type"` // "shr_with_1_disk_protect", "raid5", …
	Desc       string `json:"desc"`        // "SHR" / "Basic" / …
	FSType     string `json:"fs_type"`     // "btrfs", "ext4"
	NumID      int    `json:"num_id"`
	PoolPath   string `json:"pool_path,omitempty"`
	RaidType   string `json:"raidType,omitempty"` // "single", "shr1", "raid5"
	SpacePath  string `json:"space_path,omitempty"`
	IsWritable bool   `json:"is_writable,omitempty"`
	Size       struct {
		FreeInode  string `json:"free_inode"`
		TotalInode string `json:"total_inode"`
		Total      string `json:"total"` // bytes, as string
		Used       string `json:"used"`  // bytes, as string
	} `json:"size"`
	Status        string `json:"status"`         // "normal", "attention", "crashed"
	SummaryStatus string `json:"summary_status"` // sometimes more specific
}

// StoragePool groups physical disks into a redundancy group.
type StoragePool struct {
	ID         string   `json:"id"`
	DeviceType string   `json:"device_type"`
	RaidType   string   `json:"raidType,omitempty"`
	NumID      int      `json:"num_id"`
	Disks      []string `json:"disks,omitempty"`
	Pool       struct {
		Status string `json:"status"`
	} `json:"pool"`
	Size struct {
		Total string `json:"total"`
		Used  string `json:"used"`
	} `json:"size"`
	Progress      json.RawMessage `json:"progress,omitempty"`
	Status        string          `json:"status"`
	SummaryStatus string          `json:"summary_status"`
}

// Disk is a physical drive.
type Disk struct {
	ID          string `json:"id"`
	Path        string `json:"path,omitempty"`
	Device      string `json:"device,omitempty"`
	DiskType    string `json:"diskType"`
	Model       string `json:"model"`
	Vendor      string `json:"vendor"`
	Firmware    string `json:"firm,omitempty"`
	Status      string `json:"status"`
	Temperature int    `json:"temp"`
	Capacity    string `json:"capacity"` // bytes, as string
	Used        string `json:"used,omitempty"`
	Container   struct {
		Order int    `json:"order"`
		Type  string `json:"type"`
		Str   string `json:"str,omitempty"`
	} `json:"container"`
	Smart struct {
		Status string `json:"status,omitempty"`
	} `json:"smart,omitempty"`
	NumID  int    `json:"num_id,omitempty"`
	Serial string `json:"serial,omitempty"`
}

// Storage fetches volumes/pools/disks in one call via the legacy CGI path
// that DSM 7 still keeps wired up.
func (c *Client) Storage(ctx context.Context) (*Storage, error) {
	var raw json.RawMessage
	if err := c.Call(ctx, "SYNO.Storage.CGI.Storage", 1, "load_info", nil, &raw); err != nil {
		return nil, err
	}
	var out Storage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.Raw = raw
	return &out, nil
}

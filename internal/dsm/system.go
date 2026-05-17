package dsm

import (
	"context"
	"fmt"
	"net/url"
)

// SystemInfo is SYNO.Core.System.info, with the field tags DSM 7
// actually uses (firmware_ver, sys_temp, enabled_ntp — not the names
// the docs suggest).
type SystemInfo struct {
	Model           string `json:"model"`
	Serial          string `json:"serial"`
	Version         string `json:"firmware_ver"` // "DSM 7.0.1-42218 Update 7"
	FirmwareDate    string `json:"firmware_date"`
	NTPServer       string `json:"ntp_server"`
	NTPEnabled      bool   `json:"enabled_ntp"`
	TimeZone        string `json:"time_zone"`
	TimeZoneDesc    string `json:"time_zone_desc"`
	Temperature     int    `json:"sys_temp"` // celsius
	TemperatureWarn bool   `json:"temperature_warning"`
	UptimeSeconds   string `json:"up_time"`         // "hhh:mm:ss" on DSM 7
	SystemTime      string `json:"time"`            // "2026-05-17 22:31:18"
	CPUClock        int    `json:"cpu_clock_speed"` // MHz
	CPUCores        string `json:"cpu_cores"`
	CPUFamily       string `json:"cpu_family"`
	CPUSeries       string `json:"cpu_series"`
	CPUVendor       string `json:"cpu_vendor"`
	RAMTotalMB      int    `json:"ram_size"` // MiB
	SupportESATA    string `json:"support_esata"`
	USBDev          []any  `json:"usb_dev,omitempty"`
	SataDev         []any  `json:"sata_dev,omitempty"`

	// DSMVersion is kept as an alias for Version to avoid churn in
	// consumers that pre-date this rename.
	DSMVersion string `json:"-"`
	Hostname   string `json:"-"`
	Build      string `json:"-"`
}

// SystemInfo tries v3 first, then v1, then falls back to SYNO.DSM.Info
// for old firmware.
func (c *Client) SystemInfo(ctx context.Context) (*SystemInfo, error) {
	for _, v := range []int{3, 1} {
		params := url.Values{}
		params.Set("type", "all")
		var out SystemInfo
		if err := c.Call(ctx, "SYNO.Core.System", v, "info", params, &out); err == nil {
			return &out, nil
		}
	}
	// Last-resort: SYNO.DSM.Info exposes a narrower set of fields under a
	// different shape on legacy firmware.
	var legacy struct {
		Model       string `json:"model"`
		Serial      string `json:"serial"`
		Codepage    string `json:"codepage"`
		Time        string `json:"time"`
		Version     string `json:"version"`
		VersionStr  string `json:"version_string"`
		RAMSize     int    `json:"ram"`
		Temperature int    `json:"temperature"`
		UptimeSec   int64  `json:"uptime"`
	}
	if err := c.Call(ctx, "SYNO.DSM.Info", 2, "getinfo", nil, &legacy); err != nil {
		return nil, err
	}
	return &SystemInfo{
		Model:         legacy.Model,
		Serial:        legacy.Serial,
		Version:       legacy.VersionStr,
		DSMVersion:    legacy.VersionStr,
		RAMTotalMB:    legacy.RAMSize,
		Temperature:   legacy.Temperature,
		UptimeSeconds: secondsToDSMUptime(legacy.UptimeSec),
	}, nil
}

func secondsToDSMUptime(s int64) string {
	if s <= 0 {
		return ""
	}
	days := s / 86400
	s %= 86400
	hours := s / 3600
	s %= 3600
	mins := s / 60
	secs := s % 60
	return fmt.Sprintf("%d:%02d:%02d:%02d", days, hours, mins, secs)
}

// Utilization is the live counters block returned by SYNO.Core.System.Utilization.
type Utilization struct {
	CPU struct {
		FifteenMinLoad int    `json:"15min_load"`
		FiveMinLoad    int    `json:"5min_load"`
		OneMinLoad     int    `json:"1min_load"`
		OtherLoad      int    `json:"other_load"`
		SystemLoad     int    `json:"system_load"`
		UserLoad       int    `json:"user_load"`
		Device         string `json:"device"`
	} `json:"cpu"`
	Memory struct {
		AvailReal int    `json:"avail_real"`
		AvailSwap int    `json:"avail_swap"`
		Buffer    int    `json:"buffer"`
		Cached    int    `json:"cached"`
		MemoryUse int    `json:"memory_size"`
		RealUsage int    `json:"real_usage"`
		SiDisk    int    `json:"si_disk"`
		SoDisk    int    `json:"so_disk"`
		SwapUsage int    `json:"swap_usage"`
		TotalReal int    `json:"total_real"`
		TotalSwap int    `json:"total_swap"`
		Device    string `json:"device"`
	} `json:"memory"`
	Network []struct {
		Device string `json:"device"` // total | eth0 | eth1 …
		Rx     int64  `json:"rx"`     // bytes/sec
		Tx     int64  `json:"tx"`
	} `json:"network"`
	Disk struct {
		Disk []struct {
			Device      string `json:"device"` // sda, sdb …
			DisplayName string `json:"display_name,omitempty"`
			ReadAccess  int    `json:"read_access"`
			WriteAccess int    `json:"write_access"`
			ReadByte    int64  `json:"read_byte"`
			WriteByte   int64  `json:"write_byte"`
			Util        int    `json:"util"`
		} `json:"disk"`
		Total struct {
			Device      string `json:"device"`
			ReadAccess  int    `json:"read_access"`
			WriteAccess int    `json:"write_access"`
			ReadByte    int64  `json:"read_byte"`
			WriteByte   int64  `json:"write_byte"`
			Util        int    `json:"util"`
		} `json:"total"`
	} `json:"disk"`
	Space struct {
		Total struct {
			Device      string `json:"device"`
			ReadAccess  int    `json:"read_access"`
			WriteAccess int    `json:"write_access"`
			ReadByte    int64  `json:"read_byte"`
			WriteByte   int64  `json:"write_byte"`
			Util        int    `json:"util"`
		} `json:"total"`
	} `json:"space"`
	Time int64 `json:"time"`
}

// Utilization returns a single sample of live counters.
func (c *Client) Utilization(ctx context.Context) (*Utilization, error) {
	var out Utilization
	params := url.Values{}
	params.Set("type", "current")
	if err := c.Call(ctx, "SYNO.Core.System.Utilization", 1, "get", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Reboot triggers a system reboot. Requires admin privileges.
func (c *Client) Reboot(ctx context.Context) error {
	return c.Call(ctx, "SYNO.Core.System", 1, "reboot", nil, nil)
}

// Shutdown powers off the device. Requires admin privileges.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.Call(ctx, "SYNO.Core.System", 1, "shutdown", nil, nil)
}

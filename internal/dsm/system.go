package dsm

import (
	"context"
	"encoding/json"
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

// UtilizationHistory returns a slice of historical utilisation samples for
// the given window: "hour", "day", "week", "month", or "year". DSM
// firmwares disagree about the response shape:
//
//   - Modern firmwares return data as an array of per-slot Utilization
//     objects (or a `{ "items": [...] }` envelope).
//   - Older firmwares return a single Utilization object where the scalar
//     fields (cpu.user_load, memory.real_usage, disk.total.util, …) are
//     replaced by arrays of values, one per slot in the window.
//
// We try the modern shape first and fall back to the embedded-series shape
// when decoding it as a list fails.
func (c *Client) UtilizationHistory(ctx context.Context, window string) ([]Utilization, error) {
	switch window {
	case "hour", "day", "week", "month", "year":
	default:
		return nil, fmt.Errorf("dsm: unknown utilization window %q", window)
	}
	params := url.Values{}
	params.Set("type", window)
	var raw json.RawMessage
	if err := c.Call(ctx, "SYNO.Core.System.Utilization", 1, "get", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	// Shape A: top-level array of Utilization.
	var arr []Utilization
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}
	// Shape B: wrapped in { "items": [...] } or { "data": [...] }.
	var wrap struct {
		Items []Utilization   `json:"items"`
		Data  []Utilization   `json:"data"`
		List  []Utilization   `json:"list"`
		Raw   json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		if len(wrap.Items) > 0 {
			return wrap.Items, nil
		}
		if len(wrap.Data) > 0 {
			return wrap.Data, nil
		}
		if len(wrap.List) > 0 {
			return wrap.List, nil
		}
	}
	// Shape C: a single Utilization object where scalar fields are
	// actually arrays of per-slot values. We project that back into a
	// list of Utilization samples by index-aligning the series.
	samples, ok := expandSeriesSample(raw)
	if ok && len(samples) > 0 {
		return samples, nil
	}
	// Last resort: a single Utilization with scalar fields — return a
	// one-element slice so callers don't have to special-case empty
	// responses.
	var one Utilization
	if err := json.Unmarshal(raw, &one); err == nil {
		return []Utilization{one}, nil
	}
	return nil, fmt.Errorf("dsm: unrecognised UtilizationHistory shape")
}

// expandSeriesSample interprets the embedded-series response shape: a
// single object where cpu / memory / disk / network fields hold parallel
// arrays of values. The number of slots is taken from the longest array
// we can find; shorter arrays are zero-extended at their tail.
//
// We only consume the fields the Resource Monitor renders today (CPU
// loads, Memory real_usage, Network total rx/tx, Disk total util). The
// full Utilization struct is preserved for shape A; this function is a
// pure backward-compat path.
func expandSeriesSample(raw json.RawMessage) ([]Utilization, bool) {
	var probe struct {
		CPU struct {
			UserLoad   json.RawMessage `json:"user_load"`
			SystemLoad json.RawMessage `json:"system_load"`
			OtherLoad  json.RawMessage `json:"other_load"`
		} `json:"cpu"`
		Memory struct {
			RealUsage json.RawMessage `json:"real_usage"`
		} `json:"memory"`
		Network []struct {
			Device string          `json:"device"`
			Rx     json.RawMessage `json:"rx"`
			Tx     json.RawMessage `json:"tx"`
		} `json:"network"`
		Disk struct {
			Total struct {
				Util json.RawMessage `json:"util"`
			} `json:"total"`
		} `json:"disk"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, false
	}
	cpuUser := decodeIntSeries(probe.CPU.UserLoad)
	cpuSys := decodeIntSeries(probe.CPU.SystemLoad)
	cpuOther := decodeIntSeries(probe.CPU.OtherLoad)
	memUse := decodeIntSeries(probe.Memory.RealUsage)
	diskUtil := decodeIntSeries(probe.Disk.Total.Util)
	var rx, tx []int64
	for _, n := range probe.Network {
		if n.Device == "total" {
			rx = decodeInt64Series(n.Rx)
			tx = decodeInt64Series(n.Tx)
			break
		}
	}
	n := 0
	for _, s := range [][]int{cpuUser, cpuSys, cpuOther, memUse, diskUtil} {
		if len(s) > n {
			n = len(s)
		}
	}
	if len(rx) > n {
		n = len(rx)
	}
	if len(tx) > n {
		n = len(tx)
	}
	if n == 0 {
		return nil, false
	}
	out := make([]Utilization, n)
	for i := 0; i < n; i++ {
		out[i].CPU.UserLoad = atOrZero(cpuUser, i)
		out[i].CPU.SystemLoad = atOrZero(cpuSys, i)
		out[i].CPU.OtherLoad = atOrZero(cpuOther, i)
		out[i].Memory.RealUsage = atOrZero(memUse, i)
		out[i].Disk.Total.Util = atOrZero(diskUtil, i)
		if i < len(rx) || i < len(tx) {
			out[i].Network = []struct {
				Device string `json:"device"`
				Rx     int64  `json:"rx"`
				Tx     int64  `json:"tx"`
			}{{
				Device: "total",
				Rx:     atOrZero64(rx, i),
				Tx:     atOrZero64(tx, i),
			}}
		}
	}
	return out, true
}

// decodeIntSeries parses either a JSON array of ints or a single int into
// an []int. Empty / null / non-numeric → nil.
func decodeIntSeries(b json.RawMessage) []int {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var arr []int
	if err := json.Unmarshal(b, &arr); err == nil {
		return arr
	}
	var one int
	if err := json.Unmarshal(b, &one); err == nil {
		return []int{one}
	}
	return nil
}

func decodeInt64Series(b json.RawMessage) []int64 {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var arr []int64
	if err := json.Unmarshal(b, &arr); err == nil {
		return arr
	}
	var one int64
	if err := json.Unmarshal(b, &one); err == nil {
		return []int64{one}
	}
	return nil
}

func atOrZero(s []int, i int) int {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func atOrZero64(s []int64, i int) int64 {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// Reboot triggers a system reboot. Requires admin privileges.
func (c *Client) Reboot(ctx context.Context) error {
	return c.Call(ctx, "SYNO.Core.System", 1, "reboot", nil, nil)
}

// Shutdown powers off the device. Requires admin privileges.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.Call(ctx, "SYNO.Core.System", 1, "shutdown", nil, nil)
}

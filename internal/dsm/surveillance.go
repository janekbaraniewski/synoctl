package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// Camera is one entry from SYNO.SurveillanceStation.Camera.list — a
// configured Surveillance Station camera. Several legacy fields are
// returned as ints meaning bool; flexBool covers the drift across
// Surveillance Station 8.x / 9.x.
type Camera struct {
	ID            int      `json:"id"`
	Name          string   `json:"newName"`
	IP            string   `json:"ip,omitempty"`
	Port          int      `json:"port,omitempty"`
	Model         string   `json:"model,omitempty"`
	Vendor        string   `json:"vendor,omitempty"`
	Status        int      `json:"status,omitempty"` // 1 = enabled, 7 = disconnected, etc.
	Enabled       flexBool `json:"enabled,omitempty"`
	Recording     flexBool `json:"recStatus,omitempty"`
	HasPTZ        flexBool `json:"ptzCap,omitempty"`
	Resolution    string   `json:"resolution,omitempty"`
	FPS           int      `json:"fps,omitempty"`
	DSPath        string   `json:"dsPath,omitempty"`
	StreamURL     string   `json:"snapshot_path,omitempty"`
	VolumeSpace   string   `json:"volume_space,omitempty"`
	DeviceType    int      `json:"deviceType,omitempty"`
	Group         string   `json:"group,omitempty"`
	LastConnected string   `json:"last_connect_time,omitempty"`
}

// Cameras lists Surveillance Station cameras via
// SYNO.SurveillanceStation.Camera "List" v9. Returns an empty slice (and
// nil error) when the device does not advertise the API — Surveillance
// Station is an optional package.
func (c *Client) Cameras(ctx context.Context) ([]Camera, error) {
	const api = "SYNO.SurveillanceStation.Camera"
	if !c.Supports(api) {
		return []Camera{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("blIncludeDeletedCam", "false")
	params.Set("privCamType", "1")
	params.Set("blGetSnapshot", "false")
	var resp struct {
		Cameras []Camera `json:"cameras"`
		Total   int      `json:"total"`
	}
	// v9 is the modern signature on Surveillance Station 9.x; v8 is a
	// compatible fallback for older installs.
	if err := c.Call(ctx, api, 9, "List", params, &resp); err == nil {
		return resp.Cameras, nil
	}
	if err := c.Call(ctx, api, 8, "List", params, &resp); err != nil {
		return nil, err
	}
	return resp.Cameras, nil
}

// Recording is one entry from SYNO.SurveillanceStation.Recording.list — a
// recorded clip on disk. Timestamps are epoch seconds (DSM-local time).
type Recording struct {
	ID         int      `json:"id"`
	CameraID   int      `json:"cameraId"`
	CameraName string   `json:"camera_name,omitempty"`
	StartTime  int64    `json:"startTime"`
	StopTime   int64    `json:"stopTime"`
	Length     int      `json:"length,omitempty"` // seconds
	FileSize   int64    `json:"fileSize,omitempty"`
	FilePath   string   `json:"filePath,omitempty"`
	Reason     int      `json:"reason,omitempty"` // 1=continuous, 2=motion, 3=alarm, …
	Status     int      `json:"status,omitempty"`
	HasArchive flexBool `json:"hasArchive,omitempty"`
	Locked     flexBool `json:"is_locked,omitempty"`
}

// Recordings lists Surveillance Station recordings via
// SYNO.SurveillanceStation.Recording "List" v6. limit caps the number of
// rows; pass 0 for the DSM default (100). Returns an empty slice (and nil
// error) when the API is not advertised.
func (c *Client) Recordings(ctx context.Context, limit int) ([]Recording, error) {
	const api = "SYNO.SurveillanceStation.Recording"
	if !c.Supports(api) {
		return []Recording{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", strconv.Itoa(limit))
	var resp struct {
		Recordings []Recording `json:"recordings"`
		Total      int         `json:"total"`
	}
	if err := c.Call(ctx, api, 6, "List", params, &resp); err == nil {
		return resp.Recordings, nil
	}
	if err := c.Call(ctx, api, 5, "List", params, &resp); err != nil {
		return nil, err
	}
	return resp.Recordings, nil
}

// SurveillanceInfo mirrors SYNO.SurveillanceStation.Info.GetInfo —
// installation-level metadata (version, license usage, CMS role).
type SurveillanceInfo struct {
	Version      string   `json:"version,omitempty"`
	VersionBuild string   `json:"version_build,omitempty"`
	MaxCamera    int      `json:"maxCameraSupport,omitempty"`
	CameraNumber int      `json:"cameraNumber,omitempty"`
	Hostname     string   `json:"hostname,omitempty"`
	Path         string   `json:"path,omitempty"`
	CMSEnabled   flexBool `json:"cmsEnabled,omitempty"`
	IsLicensed   flexBool `json:"is_licensed,omitempty"`
	LicenseNum   int      `json:"licenseNumber,omitempty"`
	UniqueID     string   `json:"unique,omitempty"`
	Timezone     string   `json:"timezone,omitempty"`
}

// SurveillanceInfo returns installation-level facts about Surveillance
// Station via SYNO.SurveillanceStation.Info "GetInfo" v8 (with a v5
// fallback for older installs). Returns nil (and nil error) when the
// API is not advertised.
func (c *Client) SurveillanceInfo(ctx context.Context) (*SurveillanceInfo, error) {
	const api = "SYNO.SurveillanceStation.Info"
	if !c.Supports(api) {
		return nil, nil
	}
	var out SurveillanceInfo
	if err := c.Call(ctx, api, 8, "GetInfo", nil, &out); err == nil {
		return &out, nil
	}
	if err := c.Call(ctx, api, 5, "GetInfo", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// CloudSyncTask is one entry from SYNO.CloudSync.list — a Cloud Sync
// connection that mirrors a DSM share to/from a third-party cloud
// (Dropbox, Google Drive, OneDrive, S3, Backblaze B2, …).
//
// Field-name drift across Cloud Sync versions:
//   - Modern Cloud Sync (DSM 7.x) calls these "connections" and uses
//     `display_name` as the human label, with `link_type` for the
//     provider id and `current_status` for the live state.
//   - Older builds wrapped the array as `tasks` and used `name` for
//     the label. CloudSyncTasks tolerates both.
//
// link_type is a provider id; the TUI maps it to a display string via
// CloudSyncProviderName so unknown ids degrade to "Cloud" instead of
// rendering a bare integer.
//
// direction encodes sync polarity: 0 = bidirectional, 1 = upload-only
// (NAS → cloud), 2 = download-only (cloud → NAS). Older firmware uses
// the string forms; the TUI normalises with CloudSyncDirectionLabel.
type CloudSyncTask struct {
	// ID is the connection id assigned by Cloud Sync. Stable across
	// reboots; not the same as the connection's display name.
	ID int `json:"id"`

	// Name is the human label. Modern Cloud Sync ships this as
	// `display_name`; UnmarshalJSON folds either into Name.
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`

	// LinkType is the provider id (integer on modern Cloud Sync,
	// occasionally a string like "Dropbox" on legacy builds).
	LinkType   int    `json:"link_type,omitempty"`
	LinkTypeS  string `json:"link_type_s,omitempty"` // legacy stringified provider
	LinkStatus string `json:"link_status,omitempty"` // "connected" / "disconnected" / "error"
	LinkRemote string `json:"link_remote,omitempty"` // remote path on the cloud provider
	LocalPath  string `json:"local_path,omitempty"`  // DSM share path

	// CurrentStatus is the live sync state ("Up to date", "Syncing",
	// "Paused", "Error", …) as Cloud Sync renders it in its UI.
	CurrentStatus string `json:"current_status,omitempty"`

	// LastSyncTime is epoch seconds of the last successful sync round.
	// Empty/0 when the task has never completed a sync.
	LastSyncTime int64 `json:"last_sync_time,omitempty"`

	// Direction is 0 (bidirectional), 1 (upload-only), 2 (download-only).
	// Older firmwares used strings; LegacyDirection captures those.
	Direction       int    `json:"direction,omitempty"`
	LegacyDirection string `json:"sync_direction,omitempty"`

	// TotalSize is bytes synced to/from this connection (cumulative).
	TotalSize int64 `json:"total_size,omitempty"`

	// ErrorCount is the number of items currently in the error queue.
	ErrorCount int `json:"error_count,omitempty"`

	// Username + AccountID identify the cloud account behind the link.
	// Useful when the same DSM has multiple Dropboxes / Drives wired up.
	Username  string `json:"username,omitempty"`
	AccountID string `json:"account_id,omitempty"`

	// Enabled reports whether the connection is active. Some firmware
	// serialises this as "0"/"1" strings instead of true/false, hence
	// flexBool.
	Enabled flexBool `json:"enabled,omitempty"`
}

// Label returns the best human-readable name for the connection,
// preferring DisplayName (modern), falling back to Name (legacy),
// and finally to a synthetic "Connection #ID" so the row is never
// blank.
func (t CloudSyncTask) Label() string {
	if t.DisplayName != "" {
		return t.DisplayName
	}
	if t.Name != "" {
		return t.Name
	}
	return cloudSyncFallbackLabel(t.ID)
}

func cloudSyncFallbackLabel(id int) string {
	if id == 0 {
		return "Cloud Sync"
	}
	return "Connection #" + strconv.Itoa(id)
}

// CloudSyncProviderName maps a DSM link_type id to the provider's
// display string. Ids are best-effort from public docs + Cloud Sync's
// own JS — they are stable but undocumented, so unknown values fall
// back to a generic "Cloud".
//
// When firmware ships a stringified provider in LinkTypeS, the TUI
// passes that through instead of going through this map.
func CloudSyncProviderName(id int) string {
	switch id {
	case 0:
		return "Dropbox"
	case 1:
		return "Google Drive"
	case 2:
		return "OneDrive"
	case 3:
		return "Amazon S3"
	case 4:
		return "Backblaze B2"
	case 5:
		return "Box"
	case 6:
		return "Baidu Cloud"
	case 7:
		return "hubiC"
	case 8:
		return "Yandex Disk"
	case 9:
		return "MegaDisk"
	case 10:
		return "OpenStack Swift"
	case 11:
		return "WebDAV"
	case 12:
		return "hicloud S3"
	case 13:
		return "Azure Storage"
	case 14:
		return "SFR NAS Backup"
	case 15:
		return "Alibaba Cloud OSS"
	case 16:
		return "Tencent COS"
	case 17:
		return "JD Cloud"
	case 18:
		return "Rackspace"
	case 19:
		return "OneDrive for Business"
	case 20:
		return "SharePoint"
	case 21:
		return "Google Cloud Storage"
	case 22:
		return "hicloud File Service"
	}
	return "Cloud"
}

// CloudSyncDirectionLabel turns a numeric direction (or the legacy
// string form) into a short chip label suitable for table rows.
//   - 0 / "bidirectional" → "↕ bidirectional"
//   - 1 / "upload"        → "↑ upload"
//   - 2 / "download"      → "↓ download"
//
// Unknown values fall back to "—".
func CloudSyncDirectionLabel(dir int, legacy string) string {
	if legacy != "" {
		switch legacy {
		case "upload", "ULO", "upload_only":
			return "↑ upload"
		case "download", "DLO", "download_only":
			return "↓ download"
		case "bidirectional", "two_way", "two-way":
			return "↕ bidirectional"
		}
	}
	switch dir {
	case 1:
		return "↑ upload"
	case 2:
		return "↓ download"
	case 0:
		return "↕ bidirectional"
	}
	return "—"
}

// CloudSyncTasks lists Cloud Sync connections via SYNO.CloudSync
// "list" v1. Returns an empty slice (and nil error) when the package
// isn't installed — Cloud Sync is optional.
//
// Envelope drift: modern Cloud Sync wraps the array under
// `connections`, older builds use `tasks`, and a handful of in-between
// firmwares use a generic `list`. We probe in that order; on a *dsm.Error
// with code 102 (api not found) or 104 (version unsupported) we fall
// through silently rather than surfacing the error — those are
// "package not installed / shape moved" signals, not real failures.
func (c *Client) CloudSyncTasks(ctx context.Context) ([]CloudSyncTask, error) {
	const api = "SYNO.CloudSync"
	if !c.Supports(api) {
		return []CloudSyncTask{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")

	var resp struct {
		Connections []CloudSyncTask `json:"connections"`
		Tasks       []CloudSyncTask `json:"tasks"`
		List        []CloudSyncTask `json:"list"`
		Total       int             `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		// Fall back to no-params variant on shape-drift errors. Some
		// Cloud Sync builds reject the offset/limit pair with code 102
		// because they don't advertise that signature.
		if e, ok := err.(*Error); ok && (e.Code == 102 || e.Code == 104) {
			if err2 := c.Call(ctx, api, 1, "list", nil, &resp); err2 == nil {
				return firstNonEmptyTasks(resp.Connections, resp.Tasks, resp.List), nil
			}
		}
		return nil, err
	}
	return firstNonEmptyTasks(resp.Connections, resp.Tasks, resp.List), nil
}

// firstNonEmptyTasks returns the first non-empty slice in declaration
// order. Mirrors the firstNonEmpty pattern in package.go but typed for
// CloudSyncTask and extended to three buckets (connections / tasks /
// list) so we can probe Cloud Sync's three known envelope shapes in
// one decode.
func firstNonEmptyTasks(a, b, c []CloudSyncTask) []CloudSyncTask {
	if len(a) > 0 {
		return a
	}
	if len(b) > 0 {
		return b
	}
	if c == nil {
		return []CloudSyncTask{}
	}
	return c
}

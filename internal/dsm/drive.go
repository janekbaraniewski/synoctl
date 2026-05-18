package dsm

import (
	"context"
	"net/url"
)

// DriveFile is one entry from SYNO.SynologyDrive.Files.list — an admin
// view of a file or folder under the Synology Drive root. owner is the
// DSM user who owns the entry; team_folder indicates a Drive team share.
type DriveFile struct {
	FileID     string   `json:"file_id"`
	Name       string   `json:"name"`
	Path       string   `json:"path,omitempty"`
	Type       string   `json:"type,omitempty"` // "file" / "dir"
	Size       int64    `json:"size,omitempty"`
	Owner      string   `json:"owner,omitempty"`
	OwnerID    int      `json:"owner_id,omitempty"`
	Modified   int64    `json:"modified_time,omitempty"`
	Created    int64    `json:"created_time,omitempty"`
	Accessed   int64    `json:"accessed_time,omitempty"`
	Versions   int      `json:"version_count,omitempty"`
	Starred    flexBool `json:"starred,omitempty"`
	Shared     flexBool `json:"shared,omitempty"`
	TeamFolder flexBool `json:"team_folder,omitempty"`
	Hash       string   `json:"hash,omitempty"`
	MimeType   string   `json:"mime_type,omitempty"`
}

// DriveFiles lists Synology Drive entries under the given path via
// SYNO.SynologyDrive.Files "list" v1. When path is empty, lists from the
// Drive root ("/"). Returns an empty slice (and nil error) when the API
// is not advertised — Synology Drive Server is an optional package.
func (c *Client) DriveFiles(ctx context.Context, path string) ([]DriveFile, error) {
	const api = "SYNO.SynologyDrive.Files"
	if !c.Supports(api) {
		return []DriveFile{}, nil
	}
	if path == "" {
		path = "/"
	}
	params := url.Values{}
	params.Set("path", path)
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("sort_by", "name")
	params.Set("sort_direction", "asc")
	var resp struct {
		Files []DriveFile `json:"files"`
		Total int         `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	// Some Drive Server builds wrap the array as "items" instead.
	if len(resp.Files) == 0 {
		var alt struct {
			Items []DriveFile `json:"items"`
		}
		if err := c.Call(ctx, api, 1, "list", params, &alt); err == nil && len(alt.Items) > 0 {
			return alt.Items, nil
		}
	}
	return resp.Files, nil
}

// DriveStats mirrors SYNO.SynologyDrive.Files.Stats.get — aggregate
// counters for Synology Drive Server (file/folder totals, storage used,
// active user count). Field names are stable across Drive Server 3.x.
type DriveStats struct {
	TotalFiles    int64 `json:"total_files,omitempty"`
	TotalFolders  int64 `json:"total_folders,omitempty"`
	TotalUsers    int   `json:"total_users,omitempty"`
	ActiveUsers   int   `json:"active_users,omitempty"`
	StorageUsed   int64 `json:"storage_used,omitempty"` // bytes
	StorageQuota  int64 `json:"storage_quota,omitempty"`
	TeamFolders   int   `json:"team_folder_count,omitempty"`
	SharedFiles   int64 `json:"shared_files,omitempty"`
	VersionUsed   int64 `json:"version_used_size,omitempty"`
	TrashUsedSize int64 `json:"trash_used_size,omitempty"`
}

// DriveStats returns aggregate Synology Drive Server counters via
// SYNO.SynologyDrive.Files.Stats "get" v1. Returns nil (and nil error)
// when the API is not advertised — older Drive Server builds (pre-3.x)
// do not expose the Stats endpoint.
func (c *Client) DriveStats(ctx context.Context) (*DriveStats, error) {
	const api = "SYNO.SynologyDrive.Files.Stats"
	if !c.Supports(api) {
		return nil, nil
	}
	var out DriveStats
	if err := c.Call(ctx, api, 1, "get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

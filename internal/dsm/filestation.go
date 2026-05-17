package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// FSEntry is one row from SYNO.FileStation.List.list (a file or folder).
type FSEntry struct {
	IsDir bool   `json:"isdir"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type,omitempty"`
	Add   struct {
		Size       int64  `json:"size"`
		Owner      OwnerInfo `json:"owner,omitempty"`
		Time       FSTime `json:"time,omitempty"`
		Perm       FSPerm `json:"perm,omitempty"`
		RealPath   string `json:"real_path,omitempty"`
		Type       string `json:"type,omitempty"`
		MountPoint string `json:"mount_point_type,omitempty"`
	} `json:"additional"`
}

// OwnerInfo captures DSM's user/group ownership block.
type OwnerInfo struct {
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
	UID   int    `json:"uid,omitempty"`
	GID   int    `json:"gid,omitempty"`
}

// FSTime captures DSM's timestamps (epoch seconds).
type FSTime struct {
	Atime  int64 `json:"atime,omitempty"`
	Mtime  int64 `json:"mtime,omitempty"`
	Ctime  int64 `json:"ctime,omitempty"`
	Crtime int64 `json:"crtime,omitempty"`
}

// FSPerm captures filesystem permission flags.
type FSPerm struct {
	POSIX int    `json:"posix"`
	AdvRight any  `json:"adv_right,omitempty"`
	ACL  any   `json:"acl,omitempty"`
}

// VolumeStatus is the per-share filesystem space block returned in the
// FileStation list_share `additional.volume_status` field.
type VolumeStatus struct {
	FreeSpace  int64 `json:"freespace,omitempty"`
	TotalSpace int64 `json:"totalspace,omitempty"`
	ReadOnly   bool  `json:"readonly,omitempty"`
}

// FileShare is one entry from SYNO.FileStation.List.list_share — the
// roots File Station exposes (typically the shared folders).
type FileShare struct {
	IsDir       bool   `json:"isdir"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Add         struct {
		Owner     OwnerInfo    `json:"owner,omitempty"`
		Time      FSTime       `json:"time,omitempty"`
		Perm      FSPerm       `json:"perm,omitempty"`
		MountType string       `json:"mount_point_type,omitempty"`
		VolStatus VolumeStatus `json:"volume_status,omitempty"`
		RealPath  string       `json:"real_path,omitempty"`
	} `json:"additional"`
}

// FileShares lists the File Station roots (top-level shares). This is
// the natural entry point for the file browser.
func (c *Client) FileShares(ctx context.Context) ([]FileShare, error) {
	params := url.Values{}
	params.Set("additional", `["real_path","owner","time","perm","mount_point_type","volume_status"]`)
	var resp struct {
		Shares []FileShare `json:"shares"`
		Total  int         `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.FileStation.List", 2, "list_share", params, &resp); err != nil {
		return nil, err
	}
	return resp.Shares, nil
}

// FileDelete deletes a single file or directory. Path must be the
// absolute DSM path (e.g. /volume1/photos/foo.jpg). `recursive` is
// required for non-empty directories.
func (c *Client) FileDelete(ctx context.Context, path string, recursive bool) error {
	params := url.Values{}
	params.Set("path", `["`+path+`"]`)
	if recursive {
		params.Set("recursive", "true")
	}
	return c.Call(ctx, "SYNO.FileStation.Delete", 2, "delete", params, nil)
}

// FileRename renames a file in place. newName is the final path component
// (not a full path).
func (c *Client) FileRename(ctx context.Context, path, newName string) error {
	params := url.Values{}
	params.Set("path", `["`+path+`"]`)
	params.Set("name", `["`+newName+`"]`)
	return c.Call(ctx, "SYNO.FileStation.Rename", 2, "rename", params, nil)
}

// ListFiles lists the contents of a folder. Path is the DSM-style
// absolute path (e.g. /volume1/photos). The result is a slice of FSEntry
// with size + timestamps populated.
func (c *Client) ListFiles(ctx context.Context, path string, offset, limit int) ([]FSEntry, int, error) {
	params := url.Values{}
	params.Set("folder_path", path)
	params.Set("offset", strconv.Itoa(offset))
	if limit <= 0 {
		limit = 500
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("sort_by", "name")
	params.Set("additional", `["real_path","size","owner","time","perm","type"]`)
	var resp struct {
		Files []FSEntry `json:"files"`
		Total int       `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.FileStation.List", 2, "list", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Files, resp.Total, nil
}

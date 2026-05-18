package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// ABTask is one entry from SYNO.ActiveBackup.Task.list — an Active
// Backup for Business task (PC, VM, file server, or Microsoft 365).
// device_type indicates the source kind; status reflects the last run.
type ABTask struct {
	TaskID         int      `json:"task_id"`
	Name           string   `json:"task_name"`
	DeviceType     string   `json:"device_type,omitempty"` // "pc" / "vmm" / "fileserver" / "m365" / "gws"
	DeviceID       int      `json:"device_id,omitempty"`
	DeviceName     string   `json:"device_name,omitempty"`
	State          string   `json:"state,omitempty"`        // "ready" / "running" / "error"
	Status         string   `json:"status,omitempty"`
	LastBackupTime int64    `json:"last_backup_time,omitempty"`
	NextBackupTime int64    `json:"next_backup_time,omitempty"`
	LastResult     string   `json:"last_backup_result,omitempty"`
	UsedSize       int64    `json:"used_size,omitempty"` // bytes
	TotalSize      int64    `json:"total_size,omitempty"`
	Enable         flexBool `json:"enable,omitempty"`
	RepoID         int      `json:"repo_id,omitempty"`
	RepoPath       string   `json:"repo_path,omitempty"`
	Schedule       string   `json:"schedule_str,omitempty"`
}

// ABTasks lists Active Backup for Business tasks via SYNO.ActiveBackup.Task
// "list" v1. Returns an empty slice (and nil error) when the API is not
// advertised — Active Backup for Business is an optional package.
func (c *Client) ABTasks(ctx context.Context) ([]ABTask, error) {
	const api = "SYNO.ActiveBackup.Task"
	if !c.Supports(api) {
		return []ABTask{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Tasks []ABTask `json:"tasks"`
		Total int      `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// ABVersion is one entry from SYNO.ActiveBackup.Version.list — a
// recoverable version (snapshot) for an Active Backup task.
type ABVersion struct {
	VersionID    int      `json:"version_id"`
	TaskID       int      `json:"task_id"`
	StartTime    int64    `json:"start_time,omitempty"`
	EndTime      int64    `json:"end_time,omitempty"`
	Status       string   `json:"status,omitempty"`
	Result       string   `json:"result,omitempty"`
	UsedSize     int64    `json:"used_size,omitempty"`
	TransferSize int64    `json:"transfer_size,omitempty"`
	Duration     int64    `json:"duration,omitempty"` // seconds
	Locked       flexBool `json:"locked,omitempty"`
	Note         string   `json:"note,omitempty"`
}

// ABVersions lists snapshot versions for an Active Backup task via
// SYNO.ActiveBackup.Version "list" v1. Returns an empty slice (and nil
// error) when the API is not advertised.
func (c *Client) ABVersions(ctx context.Context, taskID int) ([]ABVersion, error) {
	const api = "SYNO.ActiveBackup.Version"
	if !c.Supports(api) {
		return []ABVersion{}, nil
	}
	params := url.Values{}
	params.Set("task_id", strconv.Itoa(taskID))
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Versions []ABVersion `json:"versions"`
		Total    int         `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Versions, nil
}

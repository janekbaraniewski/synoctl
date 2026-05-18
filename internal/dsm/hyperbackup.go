package dsm

import (
	"context"
	"net/url"
)

// BackupTask is one entry from SYNO.Backup.Task.list — a Hyper Backup
// task. last_run / last_status come from the last backup attempt; the
// schedule block is intentionally kept opaque since DSM serializes it
// differently across Hyper Backup 2.x and 3.x.
type BackupTask struct {
	TaskID       int      `json:"task_id"`
	Name         string   `json:"name"`
	Type         string   `json:"type,omitempty"` // "data_backup" / "lun_backup"
	Status       string   `json:"status,omitempty"`
	State        string   `json:"state,omitempty"`
	Enable       flexBool `json:"enable,omitempty"`
	RepoTarget   string   `json:"repo_target,omitempty"` // "local", "rsync", "s3", …
	RepoPath     string   `json:"repo_path,omitempty"`
	RepoHost     string   `json:"repo_host,omitempty"`
	Schedule     string   `json:"schedule,omitempty"`   // human-formatted "Every day 03:00"
	LastRun      int64    `json:"last_run,omitempty"`   // epoch seconds
	NextRun      int64    `json:"next_run,omitempty"`
	LastStatus   string   `json:"last_status,omitempty"`
	LastDuration int64    `json:"last_duration,omitempty"` // seconds
	TotalSize    int64    `json:"total_size,omitempty"`    // bytes
	UsedSize     int64    `json:"used_size,omitempty"`     // bytes
	Versions     int      `json:"versions,omitempty"`
	Encrypted    flexBool `json:"encrypted,omitempty"`
}

// BackupTasks lists Hyper Backup tasks via SYNO.Backup.Task "list" v1.
// Returns an empty slice (and nil error) when SYNO.Backup.Task is not
// advertised — Hyper Backup is an optional package.
func (c *Client) BackupTasks(ctx context.Context) ([]BackupTask, error) {
	const api = "SYNO.Backup.Task"
	if !c.Supports(api) {
		return []BackupTask{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("additional", `["last_bkp_progress","is_modified","status","last_bkp_time","next_bkp_time","last_bkp_result","total_size","used_size","versions"]`)
	var resp struct {
		Tasks []BackupTask `json:"task_list"`
		Total int          `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	// Some Hyper Backup builds wrap the array as "tasks" instead.
	if len(resp.Tasks) == 0 {
		var alt struct {
			Tasks []BackupTask `json:"tasks"`
		}
		if err := c.Call(ctx, api, 1, "list", params, &alt); err == nil && len(alt.Tasks) > 0 {
			return alt.Tasks, nil
		}
	}
	return resp.Tasks, nil
}

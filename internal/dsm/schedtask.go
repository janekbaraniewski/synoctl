package dsm

import (
	"context"
	"net/url"
)

// ScheduledTask is one entry from SYNO.Core.TaskScheduler.list — a
// Task Scheduler entry (script, beep test, system reboot, S.M.A.R.T.
// test, etc.). type values include "script", "service", "reboot",
// "shutdown", "smart_test". next_trigger_time is epoch seconds on
// DSM 7.x; older DSM 6 returned an ISO string in the same field.
type ScheduledTask struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Type            string   `json:"type,omitempty"`
	Enable          flexBool `json:"enable,omitempty"`
	Owner           string   `json:"owner,omitempty"`
	OwnerUID        int      `json:"owner_uid,omitempty"`
	NextTriggerTime int64    `json:"next_trigger_time,omitempty"`
	LastRunTime     int64    `json:"last_run_time,omitempty"`
	LastRunResult   string   `json:"last_run_result,omitempty"`
	Repeat          string   `json:"repeat,omitempty"` // "daily", "weekly", "monthly", "once"
	RepeatHour      int      `json:"repeat_hour,omitempty"`
	RepeatMin       int      `json:"repeat_min,omitempty"`
	CanRun          flexBool `json:"can_run,omitempty"`
	CanEdit         flexBool `json:"can_edit,omitempty"`
	Action          string   `json:"action,omitempty"`
}

// ScheduledTasks lists Task Scheduler entries via SYNO.Core.TaskScheduler
// "list" v1. Returns an empty slice (and nil error) when the API is not
// advertised.
func (c *Client) ScheduledTasks(ctx context.Context) ([]ScheduledTask, error) {
	const api = "SYNO.Core.TaskScheduler"
	if !c.Supports(api) {
		return []ScheduledTask{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	params.Set("sort_by", "next_trigger_time")
	params.Set("sort_direction", "ASC")
	var resp struct {
		Tasks []ScheduledTask `json:"tasks"`
		Total int             `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

package dsm

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ScheduledTask is one entry from SYNO.Core.TaskScheduler.list — a
// Task Scheduler entry (script, beep test, system reboot, S.M.A.R.T.
// test, etc.). type values include "script", "service", "reboot",
// "shutdown", "smart_test". next_trigger_time arrives as epoch seconds
// or as a "YYYY-MM-DD HH:MM" string depending on firmware (DSM 7.4
// sends the string), hence flexEpoch.
type ScheduledTask struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type,omitempty"`
	Enable          flexBool  `json:"enable,omitempty"`
	Owner           string    `json:"owner,omitempty"`
	OwnerUID        int       `json:"owner_uid,omitempty"`
	NextTriggerTime flexEpoch `json:"next_trigger_time,omitempty"`
	LastRunTime     flexEpoch `json:"last_run_time,omitempty"`
	LastRunResult   string    `json:"last_run_result,omitempty"`
	Repeat          string    `json:"repeat,omitempty"` // "daily", "weekly", "monthly", "once"
	RepeatHour      int       `json:"repeat_hour,omitempty"`
	RepeatMin       int       `json:"repeat_min,omitempty"`
	CanRun          flexBool  `json:"can_run,omitempty"`
	CanEdit         flexBool  `json:"can_edit,omitempty"`
	Action          string    `json:"action,omitempty"`
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

// RunScheduledTask kicks off a scheduled task immediately, regardless of
// its schedule. Endpoint is SYNO.Core.TaskScheduler v1 `run`. DSM keys
// tasks by integer id (the same id returned by ScheduledTasks). Returns
// nil on success; the task itself runs asynchronously — observing its
// progress means polling ScheduledTasks for LastRunTime / LastRunResult
// to flip.
func (c *Client) RunScheduledTask(ctx context.Context, id int) error {
	if id <= 0 {
		return fmt.Errorf("dsm: task id is required")
	}
	params := url.Values{}
	params.Set("task_id", strconv.Itoa(id))
	return c.Call(ctx, "SYNO.Core.TaskScheduler", 1, "run", params, nil)
}

// SetScheduledTaskEnabled toggles whether a scheduled task fires on its
// configured schedule. Endpoint is SYNO.Core.TaskScheduler v1 `set` with
// just the enable flag — DSM is forgiving about omitting the rest of
// the task definition on this path, which is what lets us treat
// enable/disable as a one-shot operation instead of a full edit.
func (c *Client) SetScheduledTaskEnabled(ctx context.Context, id int, enabled bool) error {
	if id <= 0 {
		return fmt.Errorf("dsm: task id is required")
	}
	params := url.Values{}
	params.Set("task_id", strconv.Itoa(id))
	params.Set("enable", strconv.FormatBool(enabled))
	return c.Call(ctx, "SYNO.Core.TaskScheduler", 1, "set", params, nil)
}

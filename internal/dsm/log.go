package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// LogEntry is a single line from the system log.
type LogEntry struct {
	ID    int64  `json:"id"`
	Time  string `json:"time"`  // human-formatted by server
	Level string `json:"level"` // info/warn/err
	User  string `json:"user"`
	IP    string `json:"ip"`
	Event string `json:"event"`
	Descr string `json:"descr,omitempty"`
}

// LogQuery filters log entries.
type LogQuery struct {
	Source   string // "system", "connection", "fileactivity"; default "system"
	Severity string // "all", "info", "warn", "err"; default "all"
	Offset   int
	Limit    int    // default 100
	Keyword  string // free-text filter
}

// Logs fetches a page of log entries.
func (c *Client) Logs(ctx context.Context, q LogQuery) ([]LogEntry, int, error) {
	params := url.Values{}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	source := q.Source
	if source == "" {
		source = "system"
	}
	severity := q.Severity
	if severity == "" {
		severity = "all"
	}
	params.Set("logtype", source)
	params.Set("level", severity)
	params.Set("start", strconv.Itoa(q.Offset))
	params.Set("limit", strconv.Itoa(q.Limit))
	if q.Keyword != "" {
		params.Set("keyword", q.Keyword)
	}
	var resp struct {
		Items []LogEntry `json:"items"`
		Total int        `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.Core.SyslogClient.Log", 1, "list", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Items, resp.Total, nil
}

package dsm

import (
	"context"
)

// SecAdvisorReport mirrors SYNO.Core.SecurityAdvisor.Conf "get" — the
// global Security Advisor configuration plus a summary of the latest
// scan (counts by severity, last-scan timestamp).
type SecAdvisorReport struct {
	Baseline       string   `json:"baseline,omitempty"`     // "default" / "secure" / "custom"
	ScheduleEnable flexBool `json:"sched_enable,omitempty"`
	Schedule       string   `json:"sched_str,omitempty"`
	LastScanned    int64    `json:"last_scan_time,omitempty"` // epoch seconds
	LastScanResult string   `json:"last_scan_result,omitempty"`
	SafeCount      int      `json:"safe_count,omitempty"`
	InfoCount      int      `json:"info_count,omitempty"`
	WarnCount      int      `json:"warn_count,omitempty"`
	RiskCount      int      `json:"risk_count,omitempty"`
	CritCount      int      `json:"critical_count,omitempty"`
	NotifyEmail    flexBool `json:"notify_mail,omitempty"`
	NotifyPush     flexBool `json:"notify_mobile,omitempty"`
}

// SecurityAdvisorReport returns the Security Advisor configuration and
// last-scan summary via SYNO.Core.SecurityAdvisor.Conf "get" v1.
// Returns nil (and nil error) when the API is not advertised.
func (c *Client) SecurityAdvisorReport(ctx context.Context) (*SecAdvisorReport, error) {
	const api = "SYNO.Core.SecurityAdvisor.Conf"
	if !c.Supports(api) {
		return nil, nil
	}
	var out SecAdvisorReport
	if err := c.Call(ctx, api, 1, "get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecAdvisorItem is one entry from SYNO.Core.SecurityAdvisor.Checklist.list
// — a single Security Advisor check with its current severity. status
// values come from DSM: "safe" / "info" / "warning" / "risk" / "critical".
type SecAdvisorItem struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Severity    string `json:"severity,omitempty"` // alias for status on some builds
	Status      string `json:"status,omitempty"`
	Category    string `json:"category,omitempty"`
	LastScanned int64  `json:"last_scan_time,omitempty"` // epoch seconds
	Suggestion  string `json:"suggestion,omitempty"`
	Hide        flexBool `json:"hide,omitempty"`
	Result      string `json:"result,omitempty"`
}

// SecurityAdvisorItems lists the Security Advisor checklist via
// SYNO.Core.SecurityAdvisor.Checklist "list" v1. Each entry includes the
// last per-item scan time, current severity, and a short description.
// Returns an empty slice (and nil error) when the API is not advertised.
func (c *Client) SecurityAdvisorItems(ctx context.Context) ([]SecAdvisorItem, error) {
	const api = "SYNO.Core.SecurityAdvisor.Checklist"
	if !c.Supports(api) {
		return []SecAdvisorItem{}, nil
	}
	var resp struct {
		Items []SecAdvisorItem `json:"items"`
		Total int              `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", nil, &resp); err != nil {
		return nil, err
	}
	// Some DSM 7.2 builds wrap the list as "checklist" instead.
	if len(resp.Items) == 0 {
		var alt struct {
			Items []SecAdvisorItem `json:"checklist"`
		}
		if err := c.Call(ctx, api, 1, "list", nil, &alt); err == nil && len(alt.Items) > 0 {
			return alt.Items, nil
		}
	}
	return resp.Items, nil
}

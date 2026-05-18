package dsm

import (
	"context"
	"net/url"
	"strconv"
)

// NotificationSettings mirrors DSM's Control Panel → Notification page —
// which channels are enabled (Email / SMS / push / DSM-internal), the
// list of recipients, and a recent delivery-failure count when DSM
// surfaces it.
//
// DSM exposes the underlying configuration through a small family of
// per-channel "Conf" endpoints (SYNO.Core.Notification.Mail.Conf,
// SYNO.Core.Notification.SMS.Conf, …). The field names and the exact
// shape of the response have drifted across firmwares; we therefore
// pick a few plausible name variants and accept whichever one DSM
// returns. Missing channels just stay zero-valued.
type NotificationSettings struct {
	EmailEnabled flexBool `json:"mail_enable,omitempty"`
	PushEnabled  flexBool `json:"push_enable,omitempty"`
	SMSEnabled   flexBool `json:"sms_enable,omitempty"`
	DSMEnabled   flexBool `json:"dsm_enable,omitempty"`

	// Recipients is the list of email recipients DSM is configured to
	// notify. The endpoint variant names vary; we tolerate "primary_email"
	// + "secondary_email" plus a list field.
	Recipients []string `json:"recipients,omitempty"`
	Primary    string   `json:"primary_email,omitempty"`
	Secondary  string   `json:"secondary_email,omitempty"`

	// FailureCount is the recent delivery-failure count when DSM provides
	// it; 0 when not reported.
	FailureCount int `json:"recent_failure_count,omitempty"`

	// Source records which endpoint we ultimately read settings from, so
	// the view can show "(via SYNO.Core.Notification.Mail.Conf)" when the
	// modern aggregate endpoint isn't available.
	Source string `json:"-"`
}

// AllRecipients folds the per-field "primary" / "secondary" pair in
// alongside any explicit Recipients list, de-duplicated and in order.
func (s NotificationSettings) AllRecipients() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(s.Recipients)+2)
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, r := range s.Recipients {
		add(r)
	}
	add(s.Primary)
	add(s.Secondary)
	return out
}

// NotificationLog is one entry from DSM's recent-notifications list:
// when it fired, which channel(s) carried it, the severity, the message
// body, and whether DSM thinks the delivery succeeded.
//
// Severity tracks DSM's "info" / "warning" / "error" buckets; channel
// tracks "mail" / "push" / "sms" / "dsm". Delivered is the parsed
// success flag — empty/unknown stays false (the view renders that as
// "—").
type NotificationLog struct {
	Time      int64  `json:"time,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Message   string `json:"message,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Delivered bool   `json:"-"`
	Status    string `json:"status,omitempty"`
}

// NotificationSettings reads the global notification configuration.
// DSM's notification system is notoriously inconsistent across firmware
// versions — the modern "GetSettings" aggregate exists on some 7.2
// builds but not all; older boxes only expose the per-channel "Conf"
// endpoints. We try them in order and return whatever populates first,
// recording which endpoint won in Source.
//
// On any "API not advertised" / "method does not exist" / "version not
// supported" error we silently fall through to the next variant. If
// nothing produces a payload the function returns a zero-valued
// NotificationSettings (and nil error) so the view can show its
// "feature not exposed by this DSM build" empty state.
func (c *Client) NotificationSettings(ctx context.Context) (NotificationSettings, error) {
	// 1. Aggregate "GetSettings" endpoint (modern DSM 7.2 builds).
	if c.Supports("SYNO.Core.Notification.Service") {
		var out NotificationSettings
		if err := c.Call(ctx, "SYNO.Core.Notification.Service", 1, "get", nil, &out); err == nil {
			out.Source = "SYNO.Core.Notification.Service.get"
			return out, nil
		} else if !isAPIMissing(err) {
			return NotificationSettings{}, err
		}
	}

	// 2. Mail.Conf — present on virtually every DSM 6.x+ build that has
	//    Control Panel → Notification at all. The response shape uses
	//    "mail_enable" / "primary_email" / "secondary_email"; we wrap
	//    it into our richer struct via JSON tags.
	merged := NotificationSettings{}
	gotAny := false
	if c.Supports("SYNO.Core.Notification.Mail.Conf") {
		var mail NotificationSettings
		if err := c.Call(ctx, "SYNO.Core.Notification.Mail.Conf", 1, "get", nil, &mail); err == nil {
			merged.EmailEnabled = mail.EmailEnabled
			merged.Primary = mail.Primary
			merged.Secondary = mail.Secondary
			if len(mail.Recipients) > 0 {
				merged.Recipients = mail.Recipients
			}
			merged.Source = "SYNO.Core.Notification.Mail.Conf.get"
			gotAny = true
		} else if !isAPIMissing(err) {
			return NotificationSettings{}, err
		}
	}

	// 3. Push.Conf for mobile push state.
	if c.Supports("SYNO.Core.Notification.Push.Conf") {
		var push NotificationSettings
		if err := c.Call(ctx, "SYNO.Core.Notification.Push.Conf", 1, "get", nil, &push); err == nil {
			merged.PushEnabled = push.PushEnabled
			if merged.Source == "" {
				merged.Source = "SYNO.Core.Notification.Push.Conf.get"
			}
			gotAny = true
		} else if !isAPIMissing(err) {
			return NotificationSettings{}, err
		}
	}

	// 4. SMS.Conf for SMS state.
	if c.Supports("SYNO.Core.Notification.SMS.Conf") {
		var sms NotificationSettings
		if err := c.Call(ctx, "SYNO.Core.Notification.SMS.Conf", 1, "get", nil, &sms); err == nil {
			merged.SMSEnabled = sms.SMSEnabled
			if merged.Source == "" {
				merged.Source = "SYNO.Core.Notification.SMS.Conf.get"
			}
			gotAny = true
		} else if !isAPIMissing(err) {
			return NotificationSettings{}, err
		}
	}

	if !gotAny {
		return NotificationSettings{}, nil
	}
	return merged, nil
}

// NotificationLog returns the last `limit` notifications DSM has on
// record. DSM does not consistently expose a single "recent
// notifications" endpoint across firmware versions — the closest
// thing is the per-channel SMS/Push delivery log, and on newer builds
// SYNO.Core.Notification.History. We try the history endpoint first
// then fall back to whatever the channel-scoped ones return, mapping
// the result into our common NotificationLog shape.
//
// Returns an empty slice (and nil error) when nothing is exposed —
// that's the documented "feature not surfaced by this DSM build"
// case and the view renders an empty state for it.
func (c *Client) NotificationLog(ctx context.Context, limit int) ([]NotificationLog, error) {
	if limit <= 0 {
		limit = 50
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", "0")

	// 1. SYNO.Core.Notification.History — the closest thing to a unified
	//    log on modern DSM. Field names: "logs" wrapper, items keyed by
	//    time / severity / channel / message.
	if c.Supports("SYNO.Core.Notification.History") {
		var resp struct {
			Logs  []NotificationLog `json:"logs"`
			Items []NotificationLog `json:"items"`
			Total int               `json:"total"`
		}
		if err := c.Call(ctx, "SYNO.Core.Notification.History", 1, "list", params, &resp); err == nil {
			if len(resp.Logs) > 0 {
				markDelivered(resp.Logs)
				return resp.Logs, nil
			}
			if len(resp.Items) > 0 {
				markDelivered(resp.Items)
				return resp.Items, nil
			}
		} else if !isAPIMissing(err) {
			return nil, err
		}
	}

	// 2. SMS.Log + Push.Log on legacy DSM. Either may be absent; we
	//    concatenate whichever returns.
	var out []NotificationLog
	if c.Supports("SYNO.Core.Notification.SMS.Conf") {
		var resp struct {
			Items []NotificationLog `json:"items"`
		}
		if err := c.Call(ctx, "SYNO.Core.Notification.SMS.Conf", 1, "list", params, &resp); err == nil {
			for i := range resp.Items {
				if resp.Items[i].Channel == "" {
					resp.Items[i].Channel = "sms"
				}
			}
			out = append(out, resp.Items...)
		} else if !isAPIMissing(err) {
			return nil, err
		}
	}
	if c.Supports("SYNO.Core.Notification.Push.Conf") {
		var resp struct {
			Items []NotificationLog `json:"items"`
		}
		if err := c.Call(ctx, "SYNO.Core.Notification.Push.Conf", 1, "list", params, &resp); err == nil {
			for i := range resp.Items {
				if resp.Items[i].Channel == "" {
					resp.Items[i].Channel = "push"
				}
			}
			out = append(out, resp.Items...)
		} else if !isAPIMissing(err) {
			return nil, err
		}
	}
	markDelivered(out)
	return out, nil
}

// markDelivered backfills the Delivered bool from the textual Status
// field — DSM uses "success" / "ok" / "delivered" interchangeably for
// the happy path; anything else is treated as a failure.
func markDelivered(logs []NotificationLog) {
	for i := range logs {
		switch logs[i].Status {
		case "success", "ok", "delivered", "sent":
			logs[i].Delivered = true
		default:
			logs[i].Delivered = false
		}
	}
}

// isAPIMissing reports whether a DSM error is one of the "API/method/
// version not available on this firmware" codes, so callers can fall
// through to a different endpoint variant without surfacing the error.
func isAPIMissing(err error) bool {
	e, ok := err.(*Error)
	if !ok {
		return false
	}
	switch e.Code {
	case 102, 103, 104:
		return true
	}
	return false
}

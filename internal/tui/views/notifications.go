package views

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// NotificationsView shows DSM's Control Panel → Notification surface as a
// two-part screen: a stats card with channel on/off chips and recipient
// list at the top, and a filterable list of recent notifications below.
// Both halves degrade gracefully when DSM doesn't expose them — the
// notification endpoints vary widely across firmware versions, and an
// empty state is a valid outcome.

type notifSettingsMsg struct {
	S   dsm.NotificationSettings
	Err error
}
type notifLogMsg struct {
	L   []dsm.NotificationLog
	Err error
}

type NotificationsView struct {
	ctx Ctx

	settings    dsm.NotificationSettings
	settingsErr error
	settingsSet bool

	logs   []dsm.NotificationLog
	logErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.NotificationLog
}

// NewNotifications constructs the notifications view.
func NewNotifications(c Ctx) tui.View { return &NotificationsView{ctx: c} }

func (v *NotificationsView) Name() string                   { return "notifications" }
func (v *NotificationsView) Title() string                  { return "Notifications" }
func (v *NotificationsView) Icon() string                   { return "✉" }
func (v *NotificationsView) RefreshInterval() time.Duration { return 2 * time.Minute }
func (v *NotificationsView) Bindings() []key.Binding        { return BaseBindings() }
func (v *NotificationsView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *NotificationsView) Init() tea.Cmd {
	return tea.Batch(v.fetchSettings(), v.fetchLog())
}

func (v *NotificationsView) fetchSettings() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (dsm.NotificationSettings, error) {
			return c.NotificationSettings(ctx)
		},
		func(s dsm.NotificationSettings, err error) tea.Msg {
			return notifSettingsMsg{S: s, Err: err}
		},
	)
}

func (v *NotificationsView) fetchLog() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.NotificationLog, error) {
			return c.NotificationLog(ctx, 50)
		},
		func(l []dsm.NotificationLog, err error) tea.Msg {
			return notifLogMsg{L: l, Err: err}
		},
	)
}

func (v *NotificationsView) filtered() []dsm.NotificationLog {
	if v.filter.Value() == "" {
		return v.logs
	}
	out := make([]dsm.NotificationLog, 0, len(v.logs))
	for _, l := range v.logs {
		if MatchesAll(v.filter.Value(), l.Severity, l.Channel, l.Message, l.Subject, l.Recipient, l.Status) {
			out = append(out, l)
		}
	}
	return out
}

func (v *NotificationsView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}
	if v.filter.IsActive() {
		before := v.filter.Value()
		if v.filter.Update(msg) {
			if v.filter.Value() != before {
				v.cursor = 0
			}
			return v, nil
		}
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchSettings(), v.fetchLog())
	case notifSettingsMsg:
		v.settings, v.settingsErr = m.S, m.Err
		v.settingsSet = true
		v.loaded = true
	case notifLogMsg:
		v.logs, v.logErr = m.L, m.Err
		v.loaded = true
		v.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.filtered())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchSettings(), v.fetchLog())
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				l := rows[v.cursor]
				v.detail = &l
			}
		}
	}
	return v, nil
}

func (v *NotificationsView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// hasAnyChannel returns true when DSM gave us at least one channel
// signal — used to decide between the channel-chip card and the
// "feature not exposed" empty state.
func (v *NotificationsView) hasAnyChannel() bool {
	s := v.settings
	return s.EmailEnabled.Bool() || s.PushEnabled.Bool() || s.SMSEnabled.Bool() ||
		s.DSMEnabled.Bool() || len(s.AllRecipients()) > 0 || s.Source != ""
}

func (v *NotificationsView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderNotificationDetail(t, width, *v.detail)
	}

	// Both halves empty, no errors, fully loaded → DSM doesn't surface
	// notifications on this build at all. Single empty state.
	if v.loaded && !v.hasAnyChannel() && len(v.logs) == 0 &&
		v.settingsErr == nil && v.logErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"✉  Notifications",
			"DSM didn't return any notification configuration or recent events.",
			"Control Panel → Notification on the NAS lets you wire up email / push / SMS."), height)
	}

	logs := v.filtered()
	var parts []string
	parts = append(parts, v.renderSettingsCard(width))
	parts = append(parts, "")
	parts = append(parts, sectionHeader(t, width, "Recent notifications", len(logs), v.logErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(logs) == 0 {
		parts = append(parts, "  "+muted(t, "(none reported by this DSM build)"))
	}
	for i, l := range logs {
		parts = append(parts, v.renderRow(l, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *NotificationsView) renderSettingsCard(width int) string {
	t := v.ctx.Theme
	if !v.settingsSet {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Notification channels ") + "\n  " + muted(t, "loading…"))
	}
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	chips := strings.Join([]string{
		channelChip(t, "Email", v.settings.EmailEnabled.Bool()),
		channelChip(t, "Push", v.settings.PushEnabled.Bool()),
		channelChip(t, "SMS", v.settings.SMSEnabled.Bool()),
		channelChip(t, "DSM", v.settings.DSMEnabled.Bool()),
	}, "  ")

	recipients := v.settings.AllRecipients()
	recipientLine := mu.Render("Recipients:") + " " + text.Render("—")
	if len(recipients) > 0 {
		recipientLine = mu.Render("Recipients:") + " " + text.Render(strings.Join(recipients, ", "))
	}
	failLine := ""
	if v.settings.FailureCount > 0 {
		failLine = "\n" + t.HealthStyle("warning").Render("  "+itoaShort(v.settings.FailureCount)+" recent delivery failures")
	}
	body := t.Title().Render(" Notification channels ") + "\n" + chips + "\n" + recipientLine + failLine
	if v.settingsErr != nil {
		body += "\n" + errLine(t, v.settingsErr)
	}
	return t.Card(false).Width(width - 2).Render(body)
}

// channelChip renders a "Email on / Push off" style chip pair. The
// label colour stays Text; only the on/off state is colourised so
// the eye lands on the actual signal.
func channelChip(t tui.Theme, label string, on bool) string {
	state := "disabled"
	if on {
		state = "enabled"
	}
	label = lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(label)
	pill := t.HealthStyle(state)
	tag := "off"
	if on {
		tag = "on"
	}
	return label + " " + pill.Render(" "+tag+" ")
}

func (v *NotificationsView) renderRow(l dsm.NotificationLog, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	sev := l.Severity
	if sev == "" {
		sev = "info"
	}
	when := "—"
	if l.Time > 0 {
		when = time.Unix(l.Time, 0).Format("2006-01-02 15:04")
	}
	channel := l.Channel
	if channel == "" {
		channel = "—"
	}
	msg := l.Message
	if msg == "" {
		msg = l.Subject
	}
	delivered := "—"
	if l.Status != "" {
		if l.Delivered {
			delivered = "ok"
		} else {
			delivered = "failed"
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		severityStyle(t, sev).Render(padRight(strings.ToUpper(sev), 5)), " ",
		padRight(mu.Render(channel), 8), " ",
		padRight(mu.Render(when), 18), " ",
		padRight(text.Render(clipTo(msg, 48)), 48), " ",
		t.HealthStyle(deliveredHealth(delivered)).Render(delivered),
	)
}

// deliveredHealth maps our internal "ok / failed / —" to a HealthStyle
// key (HealthStyle only knows the canonical health vocabulary).
func deliveredHealth(s string) string {
	switch s {
	case "ok":
		return "ok"
	case "failed":
		return "error"
	default:
		return ""
	}
}

// Inspect implements tui.Inspector — recent notifications often have a
// longer message body than fits in a row, so a side preview is useful.
func (v *NotificationsView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	logs := v.filtered()
	if v.cursor < 0 || v.cursor >= len(logs) {
		return muted(t, "  (no selection)")
	}
	return renderNotificationInspect(t, width, logs[v.cursor])
}

func renderNotificationDetail(t tui.Theme, width int, l dsm.NotificationLog) string {
	if width < 60 {
		width = 60
	}
	sev := l.Severity
	if sev == "" {
		sev = "info"
	}
	when := "—"
	if l.Time > 0 {
		when = time.Unix(l.Time, 0).Format("2006-01-02 15:04:05")
	}
	delivered := "unknown"
	if l.Status != "" {
		if l.Delivered {
			delivered = "delivered"
		} else {
			delivered = "failed"
		}
	}
	title := l.Subject
	if title == "" {
		title = clipTo(l.Message, 60)
	}
	if title == "" {
		title = "(no subject)"
	}
	parts := []string{
		hero(t, width, "✉", title, sev, l.Channel),
		propsCard(t, width, " Properties ", [][2]string{
			{"When", when},
			{"Severity", sev},
			{"Channel", l.Channel},
			{"Recipient", l.Recipient},
			{"Status", l.Status},
			{"Delivered", delivered},
		}),
	}
	if l.Message != "" {
		body := t.Title().Render(" Message ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), l.Message, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width,
		"  esc to go back · acknowledging / clearing notifications isn't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderNotificationInspect(t tui.Theme, width int, l dsm.NotificationLog) string {
	_ = width
	sev := l.Severity
	if sev == "" {
		sev = "info"
	}
	when := "—"
	if l.Time > 0 {
		when = time.Unix(l.Time, 0).Format("2006-01-02 15:04:05")
	}
	delivered := "—"
	if l.Status != "" {
		if l.Delivered {
			delivered = "delivered"
		} else {
			delivered = "failed"
		}
	}
	msg := l.Message
	if msg == "" {
		msg = l.Subject
	}
	return strings.Join([]string{
		t.Title().Render(" Notification "),
		"  " + severityStyle(t, sev).Render(strings.ToUpper(sev)),
		muted(t, "  "+coalesce(l.Channel, "—")),
		"",
		muted(t, "  When      ") + when,
		muted(t, "  Recipient ") + coalesce(l.Recipient, "—"),
		muted(t, "  Status    ") + coalesce(l.Status, "—"),
		muted(t, "  Result    ") + delivered,
		"",
		muted(t, "  Message"),
		"  " + clipTo(msg, 60),
	}, "\n")
}

package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// renderUserDetail draws the per-user inspector.
func renderUserDetail(t tui.Theme, width int, u dsm.User) string {
	if width < 60 {
		width = 60
	}
	expired := u.Expired
	if expired == "" {
		expired = "normal"
	}
	parts := []string{
		hero(t, width, "◐", u.Name, expired, u.Description),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("password never expires", u.PasswordNeverExpire),
	}))
	parts = append(parts, propsCard(t, width, " Account ", [][2]string{
		{"Username", u.Name},
		{"UID", fmt.Sprintf("%d", u.UID)},
		{"Email", u.Email},
		{"Description", u.Description},
		{"Expiry state", u.Expired},
	}))
	if len(u.Groups) > 0 {
		body := t.Title().Render(" Groups ") + "\n  " +
			lipgloss.NewStyle().Foreground(t.Text).Render(strings.Join(u.Groups, ", "))
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · user-write actions land in the next pass"))
	return strings.Join(parts, "\n")
}

// renderServiceDetail draws the per-service inspector.
func renderServiceDetail(t tui.Theme, width int, s dsm.Service) string {
	if width < 60 {
		width = 60
	}
	state := s.EnableStatus
	if state == "" {
		state = "unknown"
	}
	parts := []string{
		hero(t, width, "⌬", s.DisplayName(), state, s.ID),
	}
	parts = append(parts, propsCard(t, width, " Properties ", [][2]string{
		{"Service ID", s.ID},
		{"Display name", s.DisplayName()},
		{"Display key", s.DisplayNameSectionKey},
		{"Enable state", s.EnableStatus},
		{"Togglable", yesNo(s.Toggleable())},
	}))
	hint := "  esc to go back · use [e] to enable / [d] to disable from the list view"
	if !s.Toggleable() {
		hint = "  esc to go back · this service is 'static' — DSM runs it unconditionally"
	}
	parts = append(parts, noteCard(t, width, hint))
	return strings.Join(parts, "\n")
}

// renderNetworkDetail draws the per-interface inspector.
func renderNetworkDetail(t tui.Theme, width int, n dsm.NetworkInterface) string {
	if width < 60 {
		width = 60
	}
	speed := "—"
	if n.Speed > 0 {
		speed = fmt.Sprintf("%d Mbit/s", n.Speed)
	}
	parts := []string{
		hero(t, width, "⇄", n.IFName, n.Status, n.Type),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("DHCP", n.UseDHCP),
	}))
	parts = append(parts, propsCard(t, width, " Addressing ", [][2]string{
		{"Interface", n.IFName},
		{"Type", n.Type},
		{"IPv4", n.IP},
		{"Mask", n.Mask},
		{"Gateway", n.Gateway},
		{"MAC", n.MAC},
		{"MTU", iOrDash(n.MTU)},
		{"Speed", speed},
		{"Status", n.Status},
	}))
	parts = append(parts, noteCard(t, width,
		"  esc to go back · DHCP/MTU edits aren't wired (require admin scope changes)"))
	return strings.Join(parts, "\n")
}

// renderPackageDetail draws the per-package inspector.
func renderPackageDetail(t tui.Theme, width int, p dsm.Package) string {
	if width < 60 {
		width = 60
	}
	status := p.Status
	if status == "" {
		status = "unknown"
	}
	parts := []string{
		hero(t, width, "▣", p.Name, status, p.Version),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("beta", p.Beta),
		chip("user-uninstallable", p.CtlUninstall),
	}))
	props := [][2]string{
		{"Identifier", p.ID},
		{"Name", p.Name},
		{"Version", p.Version},
		{"Maintainer", p.Maintainer},
		{"Status", p.Status},
	}
	if p.Timestamp > 0 {
		props = append(props, [2]string{"Installed", time.Unix(p.Timestamp/1000, 0).Format("2006-01-02 15:04")})
	}
	parts = append(parts, propsCard(t, width, " Properties ", props))
	if p.Description != "" {
		body := t.Title().Render(" Description ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), p.Description, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	hint := "  esc to go back · use [s]/[x]/[R] from the list to start/stop/restart"
	parts = append(parts, noteCard(t, width, hint))
	return strings.Join(parts, "\n")
}

// renderLogDetail draws a single log entry as a stack of labelled panels
// rather than the JSON envelope. We split out the event description into
// its own card so multi-line messages have room to breathe.
func renderLogDetail(t tui.Theme, width int, e dsm.LogEntry) string {
	if width < 60 {
		width = 60
	}
	level := e.Level
	if level == "" {
		level = "info"
	}
	parts := []string{
		hero(t, width, "≡", e.Time, level, e.Event),
		propsCard(t, width, " Metadata ", [][2]string{
			{"Time", e.Time},
			{"Level", level},
			{"User", e.User},
			{"IP", e.IP},
			{"Event", e.Event},
		}),
	}
	if e.Descr != "" {
		body := t.Title().Render(" Description ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), e.Descr, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · / from list to filter"))
	return strings.Join(parts, "\n")
}

// coalesce returns the first non-empty argument.
func coalesce(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func iOrDash(i int) string {
	if i == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", i)
}

func wrap(style lipgloss.Style, s string, width int) string {
	if width <= 0 {
		return ""
	}
	// Simple word wrap.
	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len()+len(w)+1 > width {
			lines = append(lines, "  "+cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, "  "+cur.String())
	}
	return style.Render(strings.Join(lines, "\n"))
}

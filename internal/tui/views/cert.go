package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// CertsView lists DSM-managed TLS certificates. Certs near expiry
// (valid_to within 30 days) are coloured t.Warn so they catch the eye
// before they break Let's-Encrypt-bound services.

type certsMsg struct {
	C   []dsm.Certificate
	Err error
}

type CertsView struct {
	ctx Ctx

	certs    []dsm.Certificate
	certsErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.Certificate
}

func NewCerts(c Ctx) tui.View { return &CertsView{ctx: c} }

func (v *CertsView) Name() string                   { return "certs" }
func (v *CertsView) Title() string                  { return "Certificates" }
func (v *CertsView) Icon() string                   { return "⛨" }
func (v *CertsView) RefreshInterval() time.Duration { return 5 * time.Minute }
func (v *CertsView) Bindings() []key.Binding        { return BaseBindings() }
func (v *CertsView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *CertsView) Init() tea.Cmd { return v.fetch() }

func (v *CertsView) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.Certificate, error) { return c.Certificates(ctx) },
		func(x []dsm.Certificate, err error) tea.Msg { return certsMsg{C: x, Err: err} },
	)
}

func (v *CertsView) filtered() []dsm.Certificate {
	if v.filter.Value() == "" {
		return v.certs
	}
	out := make([]dsm.Certificate, 0, len(v.certs))
	for _, c := range v.certs {
		if MatchesAll(v.filter.Value(), c.Subject, c.Issuer, c.IssuerOrg, c.Description, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

func (v *CertsView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
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
		return v, v.fetch()
	case certsMsg:
		v.certs, v.certsErr = m.C, m.Err
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
			return v, v.fetch()
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				c := rows[v.cursor]
				v.detail = &c
			}
		}
	}
	return v, nil
}

func (v *CertsView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *CertsView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderCertificateDetail(t, width, *v.detail)
	}

	if v.loaded && len(v.certs) == 0 && v.certsErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⛨  Certificates",
			"No TLS certificates returned by DSM.",
			"This is unusual — every DSM box has at least one self-signed default cert. Try refreshing with r."), height)
	}

	certs := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "Certificates", len(certs), v.certsErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(certs) == 0 {
		parts = append(parts, "  "+muted(t, "(none matching)"))
	}
	for i, c := range certs {
		parts = append(parts, v.renderRow(c, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// parseCertTime tolerates the half-dozen formats DSM has shipped over
// the years for valid_from / valid_till.
func parseCertTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	formats := []string{
		"Jan _2 15:04:05 2006 MST",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func certExpiryStyle(t tui.Theme, validTo string) lipgloss.Style {
	exp, ok := parseCertTime(validTo)
	if !ok {
		return lipgloss.NewStyle().Foreground(t.Muted)
	}
	remaining := time.Until(exp)
	switch {
	case remaining <= 0:
		return lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	case remaining <= 30*24*time.Hour:
		return lipgloss.NewStyle().Foreground(t.Warn).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(t.Text)
	}
}

func (v *CertsView) renderRow(c dsm.Certificate, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	flags := []string{}
	if c.Default.Bool() {
		flags = append(flags, lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("default"))
	}
	if c.Broken.Bool() {
		flags = append(flags, lipgloss.NewStyle().Foreground(t.Error).Bold(true).Render("broken"))
	}
	flagsRendered := strings.Join(flags, " ")
	expiry := certExpiryStyle(t, c.ValidTo).Render(c.ValidTo)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(c.Subject, 28)), 28), " ",
		padRight(mu.Render(clipTo(c.Issuer, 26)), 26), " ",
		padRight(expiry, 24), " ",
		flagsRendered,
	)
}

// Inspect implements tui.Inspector — useful here because a cert's most
// interesting facts (SAN, fingerprint algorithms, bound services) don't
// fit in a row.
func (v *CertsView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	certs := v.filtered()
	if v.cursor < 0 || v.cursor >= len(certs) {
		return muted(t, "  (no selection)")
	}
	return renderCertificateInspect(t, width, certs[v.cursor])
}

func renderCertificateDetail(t tui.Theme, width int, c dsm.Certificate) string {
	if width < 60 {
		width = 60
	}
	subject := c.Subject
	if subject == "" {
		subject = c.ID
	}
	status := "valid"
	if c.Broken.Bool() {
		status = "broken"
	} else if exp, ok := parseCertTime(c.ValidTo); ok {
		if time.Until(exp) <= 0 {
			status = "expired"
		} else if time.Until(exp) <= 30*24*time.Hour {
			status = "expiring soon"
		}
	}
	parts := []string{
		hero(t, width, "⛨", subject, status, c.IssuerOrg),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", c.ID},
			{"Description", c.Description},
			{"Subject CN", c.Subject},
			{"Issuer CN", c.Issuer},
			{"Issuer org", c.IssuerOrg},
			{"Valid from", c.ValidFrom},
			{"Valid to", c.ValidTo},
			{"Signature", c.SignatureAlgo},
			{"Key type", c.KeyType},
			{"ACME URL", c.RenewURL},
		}),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("default", c.Default.Bool()),
		chip("broken", c.Broken.Bool()),
		chip("user-deletable", c.UserDeletable.Bool()),
	}))
	// Subject Alternative Names
	sans := c.SubjectAltName
	if len(sans) == 0 && c.SubjectAltNameS != "" {
		sans = strings.Split(c.SubjectAltNameS, ",")
	}
	if len(sans) > 0 {
		var trimmed []string
		for _, s := range sans {
			trimmed = append(trimmed, strings.TrimSpace(s))
		}
		body := t.Title().Render(" SubjectAltName ") + "\n  " +
			lipgloss.NewStyle().Foreground(t.Text).Render(strings.Join(trimmed, ", "))
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	// Bound services
	if len(c.Services) > 0 {
		mu := lipgloss.NewStyle().Foreground(t.Muted)
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		var rows []string
		for _, s := range c.Services {
			disp := s.Display
			if disp == "" {
				disp = s.Service
			}
			rows = append(rows, "  "+text.Render(padRight(disp, 24))+" "+mu.Render(s.Owner))
		}
		body := t.Title().Render(" Bound services ") + "\n" + strings.Join(rows, "\n")
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · certificate write actions aren't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderCertificateInspect(t tui.Theme, width int, c dsm.Certificate) string {
	subject := c.Subject
	if subject == "" {
		subject = c.ID
	}
	expStyle := certExpiryStyle(t, c.ValidTo)
	flags := []string{}
	if c.Default.Bool() {
		flags = append(flags, "default")
	}
	if c.Broken.Bool() {
		flags = append(flags, "broken")
	}
	flagStr := "—"
	if len(flags) > 0 {
		flagStr = strings.Join(flags, ", ")
	}
	sans := c.SubjectAltName
	if len(sans) == 0 && c.SubjectAltNameS != "" {
		sans = strings.Split(c.SubjectAltNameS, ",")
	}
	sansLine := "—"
	if len(sans) > 0 {
		sansLine = clipTo(strings.Join(sans, ", "), width-4)
	}
	bound := fmt.Sprintf("%d", len(c.Services))
	return strings.Join([]string{
		t.Title().Render(" Certificate "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(subject, width-4)),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + clipTo(c.Issuer, width-4)),
		"",
		muted(t, "  Valid from ") + c.ValidFrom,
		muted(t, "  Valid to   ") + expStyle.Render(c.ValidTo),
		muted(t, "  Key type   ") + c.KeyType,
		muted(t, "  Signature  ") + c.SignatureAlgo,
		muted(t, "  Flags      ") + flagStr,
		muted(t, "  Bound      ") + bound + " services",
		"",
		muted(t, "  SAN") + "\n  " + sansLine,
	}, "\n")
}

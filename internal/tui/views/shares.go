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

// Shares is the dedicated view for shared folders. One row per share with
// path, flag chips (enc/recycle/hidden/ro/usb/sync/cloudsync), and a quota
// gauge when one is set. Drill-down opens the structured share detail.
type Shares struct {
	ctx Ctx

	shares []dsm.Share
	err    error

	base   listBase
	detail *dsm.Share
}

// NewShares constructs the shares view.
func NewShares(c Ctx) tui.View { return &Shares{ctx: c} }

func (s *Shares) Name() string                   { return "shares" }
func (s *Shares) Title() string                  { return "Shares" }
func (s *Shares) Icon() string                   { return "▦" }
func (s *Shares) RefreshInterval() time.Duration { return 30 * time.Second }
func (s *Shares) Bindings() []key.Binding        { return BaseBindings() }

func (s *Shares) Init() tea.Cmd { return s.fetch() }

func (s *Shares) fetch() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Share, error) { return c.Shares(ctx) },
		func(v []dsm.Share, err error) tea.Msg { return sharesMsg{S: v, Err: err} },
	)
}

func (s *Shares) visible() []dsm.Share {
	if s.base.FilterValue() == "" {
		return s.shares
	}
	out := make([]dsm.Share, 0, len(s.shares))
	for _, x := range s.shares {
		if MatchesAll(s.base.FilterValue(), x.Name, x.Path, x.Desc) {
			out = append(out, x)
		}
	}
	return out
}

func (s *Shares) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if s.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			s.detail = nil
		}
		return s, nil
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return s, s.fetch()
	case sharesMsg:
		s.shares, s.err = m.S, m.Err
		s.base.ClampCursor(len(s.visible()))
		return s, nil
	}
	if _, handled := s.base.HandleKey(msg, len(s.visible())); handled {
		return s, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyEnter {
		if rows := s.visible(); s.base.Cursor() < len(rows) {
			sh := rows[s.base.Cursor()]
			s.detail = &sh
		}
	}
	return s, nil
}

func (s *Shares) Render(width, height int) string {
	t := s.ctx.Theme
	if s.detail != nil {
		return renderShareDetail(t, width, height, *s.detail)
	}
	rows := s.visible()
	parts := []string{sectionHeader(t, width, "Shared folders", len(rows), s.err)}
	if s.shares == nil && s.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, sh := range rows {
		parts = append(parts, s.renderRow(width, sh, i == s.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · (browse files from the Files view)"))
	if f := s.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (s *Shares) Inspect(width, height int) string {
	rows := s.visible()
	if len(rows) == 0 || s.base.Cursor() >= len(rows) {
		return ""
	}
	t := s.ctx.Theme
	sh := rows[s.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	parts := []string{
		t.Title().Render(" " + sh.Name + " "),
		"",
		muted.Render(sh.Path),
	}
	if sh.Desc != "" {
		parts = append(parts, "", text.Render(sh.Desc))
	}
	parts = append(parts, "")
	if sh.ShareQuota > 0 {
		ratio := float64(sh.ShareQuotaUsed) / float64(sh.ShareQuota)
		parts = append(parts,
			muted.Render("Quota"),
			Gauge(t, width-2, ratio),
			fmt.Sprintf("%s used of %s",
				HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024),
				HumanBytes(uint64(sh.ShareQuota)*1024*1024)),
			"",
		)
	}
	flags := s.flagList(sh)
	if len(flags) > 0 {
		parts = append(parts, muted.Render("Flags"))
		for _, f := range flags {
			parts = append(parts, "  "+f)
		}
	}
	_ = height
	return strings.Join(parts, "\n")
}

func (s *Shares) flagList(sh dsm.Share) []string {
	t := s.ctx.Theme
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	var out []string
	if sh.IsEncrypted() {
		out = append(out, chip("encrypted", true))
	}
	if sh.EnableRecycle {
		out = append(out, chip("recycle bin", true))
	}
	if sh.Hidden {
		out = append(out, chip("hidden", true))
	}
	if sh.Readonly {
		out = append(out, chip("read-only", true))
	}
	if sh.IsUsbShare {
		out = append(out, chip("usb", true))
	}
	if sh.IsSyncShare {
		out = append(out, chip("sync", true))
	}
	if sh.IsCloudSync {
		out = append(out, chip("cloud-sync", true))
	}
	return out
}

func (s *Shares) renderRow(width int, sh dsm.Share, highlight bool) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	quota := "—"
	if sh.ShareQuota > 0 {
		ratio := float64(sh.ShareQuotaUsed) / float64(sh.ShareQuota)
		quota = fmt.Sprintf("%5.1f%%  %s / %s", ratio*100,
			HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024),
			HumanBytes(uint64(sh.ShareQuota)*1024*1024))
	}
	flags := []string{}
	if sh.IsEncrypted() {
		flags = append(flags, "enc")
	}
	if sh.EnableRecycle {
		flags = append(flags, "recycle")
	}
	if sh.Hidden {
		flags = append(flags, "hidden")
	}
	if sh.Readonly {
		flags = append(flags, "ro")
	}
	flagStr := strings.Join(flags, " ")
	_ = width
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(sh.Name), 18), " ",
		padRight(muted.Render(sh.Path), 28), " ",
		padRight(muted.Render(flagStr), 24), " ",
		padLeft(muted.Render(quota), 30),
	)
}

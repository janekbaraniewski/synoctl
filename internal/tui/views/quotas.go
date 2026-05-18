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

// QuotasView shows DSM's two flavours of storage quota in one screen:
// per-share quotas (derived from Share.list) up top, per-user quotas
// (from SYNO.Core.User.Quota.list) below. The cursor flows through
// both sections as one flat list; pressing enter opens the detail
// view for whichever kind of row is highlighted.

type shareQuotasMsg struct {
	Q   []dsm.ShareQuota
	Err error
}
type userQuotasMsg struct {
	Q   []dsm.UserQuota
	Err error
}

type QuotasView struct {
	ctx Ctx

	shares    []dsm.ShareQuota
	sharesErr error
	users     []dsm.UserQuota
	usersErr  error

	cursor int
	filter Filter
	loaded bool

	detailShare *dsm.ShareQuota
	detailUser  *dsm.UserQuota
}

// NewQuotas constructs the quotas view.
func NewQuotas(c Ctx) tui.View { return &QuotasView{ctx: c} }

func (v *QuotasView) Name() string                   { return "quotas" }
func (v *QuotasView) Title() string                  { return "Quotas" }
func (v *QuotasView) Icon() string                   { return "⊞" }
func (v *QuotasView) RefreshInterval() time.Duration { return 2 * time.Minute }
func (v *QuotasView) Bindings() []key.Binding        { return BaseBindings() }
func (v *QuotasView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *QuotasView) Init() tea.Cmd { return tea.Batch(v.fetchShares(), v.fetchUsers()) }

func (v *QuotasView) fetchShares() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.ShareQuota, error) { return c.ShareQuotas(ctx) },
		func(q []dsm.ShareQuota, err error) tea.Msg { return shareQuotasMsg{Q: q, Err: err} },
	)
}

func (v *QuotasView) fetchUsers() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.UserQuota, error) { return c.UserQuotas(ctx) },
		func(q []dsm.UserQuota, err error) tea.Msg { return userQuotasMsg{Q: q, Err: err} },
	)
}

func (v *QuotasView) filterShares() []dsm.ShareQuota {
	if v.filter.Value() == "" {
		return v.shares
	}
	out := make([]dsm.ShareQuota, 0, len(v.shares))
	for _, q := range v.shares {
		if MatchesAll(v.filter.Value(), q.Name, q.Path, q.Description) {
			out = append(out, q)
		}
	}
	return out
}

func (v *QuotasView) filterUsers() []dsm.UserQuota {
	if v.filter.Value() == "" {
		return v.users
	}
	out := make([]dsm.UserQuota, 0, len(v.users))
	for _, q := range v.users {
		volumes := ""
		for _, vol := range q.Volumes {
			volumes += " " + vol.Volume
		}
		if MatchesAll(v.filter.Value(), q.Name, volumes) {
			out = append(out, q)
		}
	}
	return out
}

// Row addressing: shares occupy the first len(filterShares) rows, then
// users occupy the rest. We track that mapping via a small enum.
type quotaRowKind int

const (
	quotaRowShare quotaRowKind = iota
	quotaRowUser
)

type quotaRow struct {
	kind  quotaRowKind
	index int
}

func (v *QuotasView) rows() []quotaRow {
	shares := v.filterShares()
	users := v.filterUsers()
	out := make([]quotaRow, 0, len(shares)+len(users))
	for i := range shares {
		out = append(out, quotaRow{quotaRowShare, i})
	}
	for i := range users {
		out = append(out, quotaRow{quotaRowUser, i})
	}
	return out
}

func (v *QuotasView) current() (quotaRow, bool) {
	rs := v.rows()
	if v.cursor < 0 || v.cursor >= len(rs) {
		return quotaRow{}, false
	}
	return rs[v.cursor], true
}

func (v *QuotasView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detailShare != nil || v.detailUser != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detailShare, v.detailUser = nil, nil
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
		return v, tea.Batch(v.fetchShares(), v.fetchUsers())
	case shareQuotasMsg:
		v.shares, v.sharesErr = m.Q, m.Err
		v.loaded = true
		v.clampCursor()
	case userQuotasMsg:
		v.users, v.usersErr = m.Q, m.Err
		v.loaded = true
		v.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rs := v.rows()
			if v.cursor < len(rs)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.rows())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchShares(), v.fetchUsers())
		case "enter":
			if row, ok := v.current(); ok {
				switch row.kind {
				case quotaRowShare:
					s := v.filterShares()[row.index]
					v.detailShare = &s
				case quotaRowUser:
					u := v.filterUsers()[row.index]
					v.detailUser = &u
				}
			}
		}
	}
	return v, nil
}

func (v *QuotasView) clampCursor() {
	n := len(v.rows())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *QuotasView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detailShare != nil {
		return renderShareQuotaDetail(t, width, *v.detailShare)
	}
	if v.detailUser != nil {
		return renderUserQuotaDetail(t, width, *v.detailUser)
	}

	if v.loaded && len(v.shares) == 0 && len(v.users) == 0 &&
		v.sharesErr == nil && v.usersErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⊞  Quotas",
			"No share or user quotas are configured on this NAS.",
			"Set per-share quotas in Control Panel → Shared Folder, or per-user limits in Control Panel → User."), height)
	}

	shares := v.filterShares()
	users := v.filterUsers()
	idx := 0

	var parts []string
	parts = append(parts, sectionHeader(t, width, "Per-share quotas", len(shares), v.sharesErr))
	if !v.loaded && len(shares) == 0 {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(shares) == 0 {
		parts = append(parts, "  "+muted(t, "(no shares with a configured quota)"))
	}
	for _, q := range shares {
		parts = append(parts, v.renderShareRow(q, idx == v.cursor, width))
		idx++
	}

	parts = append(parts, "", sectionHeader(t, width, "Per-user quotas", len(users), v.usersErr))
	if !v.loaded && len(users) == 0 {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(users) == 0 {
		parts = append(parts, "  "+muted(t, "(no users with a configured quota)"))
	}
	for _, q := range users {
		parts = append(parts, v.renderUserRow(q, idx == v.cursor))
		idx++
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *QuotasView) renderShareRow(q dsm.ShareQuota, highlight bool, width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	// Gauge sized so two columns of text fit alongside on a typical terminal.
	gaugeW := width - 70
	if gaugeW < 14 {
		gaugeW = 14
	}
	if gaugeW > 28 {
		gaugeW = 28
	}
	bar := Gauge(t, gaugeW, q.Ratio())
	used := HumanBytes(q.UsedBytes())
	total := HumanBytes(q.QuotaBytes())
	pct := fmt.Sprintf("%3d%%", q.PercentUsed)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(q.Name, 20)), 20), " ",
		bar, " ",
		padLeft(text.Render(pct), 4), " ",
		padRight(mu.Render(used+" / "+total), 22),
	)
}

func (v *QuotasView) renderUserRow(q dsm.UserQuota, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	volumes := "—"
	if len(q.Volumes) > 0 {
		vols := make([]string, 0, len(q.Volumes))
		for _, vol := range q.Volumes {
			vols = append(vols, vol.Volume)
		}
		volumes = strings.Join(vols, ", ")
	}
	used := HumanBytes(uint64(q.TotalUsed) * 1024 * 1024)
	total := "unlimited"
	pct := "—"
	if q.HasLimit {
		total = HumanBytes(uint64(q.TotalQuota) * 1024 * 1024)
		pct = fmt.Sprintf("%3d%%", q.PercentUsed)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(q.Name, 20)), 20), " ",
		padRight(mu.Render(clipTo(volumes, 22)), 22), " ",
		padLeft(text.Render(pct), 6), " ",
		padRight(mu.Render(used+" / "+total), 26),
	)
}

// Inspect implements tui.Inspector. The same row-kind switch as Render.
func (v *QuotasView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	row, ok := v.current()
	if !ok {
		return muted(t, "  (no selection)")
	}
	switch row.kind {
	case quotaRowShare:
		return renderShareQuotaInspect(t, width, v.filterShares()[row.index])
	case quotaRowUser:
		return renderUserQuotaInspect(t, width, v.filterUsers()[row.index])
	}
	return ""
}

func renderShareQuotaDetail(t tui.Theme, width int, q dsm.ShareQuota) string {
	if width < 60 {
		width = 60
	}
	parts := []string{
		hero(t, width, "⊞", q.Name, "", q.Path),
		gaugeCard(t, width, " Usage ",
			fmt.Sprintf("%d%%", q.PercentUsed), q.Ratio(),
			HumanBytes(q.UsedBytes())+" used of "+HumanBytes(q.QuotaBytes())),
		propsCard(t, width, " Properties ", [][2]string{
			{"Share", q.Name},
			{"Volume", q.Path},
			{"Quota", HumanBytes(q.QuotaBytes())},
			{"Used", HumanBytes(q.UsedBytes())},
			{"Used %", fmt.Sprintf("%d%%", q.PercentUsed)},
			{"Mode", "hard limit"},
			{"Hidden", yesNo(q.Hidden)},
			{"Description", q.Description},
		}),
		noteCard(t, width, "  esc to go back · editing quotas isn't wired up yet"),
	}
	return strings.Join(parts, "\n")
}

func renderUserQuotaDetail(t tui.Theme, width int, q dsm.UserQuota) string {
	if width < 60 {
		width = 60
	}
	usedBytes := uint64(q.TotalUsed) * 1024 * 1024
	totalBytes := uint64(q.TotalQuota) * 1024 * 1024
	subtitle := "unlimited"
	if q.HasLimit {
		subtitle = fmt.Sprintf("%d UID", q.UID)
	}
	parts := []string{
		hero(t, width, "⊞", q.Name, "", subtitle),
	}
	if q.HasLimit {
		parts = append(parts, gaugeCard(t, width, " Aggregate usage ",
			fmt.Sprintf("%d%%", q.PercentUsed),
			float64(q.PercentUsed)/100,
			HumanBytes(usedBytes)+" used of "+HumanBytes(totalBytes)))
	}
	parts = append(parts, propsCard(t, width, " Properties ", [][2]string{
		{"User", q.Name},
		{"UID", fmt.Sprintf("%d", q.UID)},
		{"Total quota", quotaOrUnlimited(q.TotalQuota)},
		{"Total used", HumanBytes(usedBytes)},
		{"Used %", fmt.Sprintf("%d%%", q.PercentUsed)},
		{"Volumes", fmt.Sprintf("%d", len(q.Volumes))},
	}))
	if len(q.Volumes) > 0 {
		var rows [][2]string
		for _, vol := range q.Volumes {
			line := HumanBytes(uint64(vol.UsedMiB)*1024*1024) + " / " + quotaOrUnlimited(vol.QuotaMiB)
			if vol.QuotaMiB > 0 {
				line += fmt.Sprintf("  (%d%%)", vol.PercentUsed)
			}
			rows = append(rows, [2]string{vol.Volume, line})
		}
		parts = append(parts, propsCard(t, width, " Per-volume ", rows))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · editing user quotas isn't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderShareQuotaInspect(t tui.Theme, width int, q dsm.ShareQuota) string {
	_ = width
	return strings.Join([]string{
		t.Title().Render(" Share quota "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + q.Name),
		muted(t, "  "+q.Path),
		"",
		muted(t, "  Quota ") + HumanBytes(q.QuotaBytes()),
		muted(t, "  Used  ") + HumanBytes(q.UsedBytes()),
		muted(t, "  %     ") + fmt.Sprintf("%d%%", q.PercentUsed),
		"",
		Gauge(t, 24, q.Ratio()),
	}, "\n")
}

func renderUserQuotaInspect(t tui.Theme, width int, q dsm.UserQuota) string {
	_ = width
	total := "unlimited"
	pct := "—"
	if q.HasLimit {
		total = HumanBytes(uint64(q.TotalQuota) * 1024 * 1024)
		pct = fmt.Sprintf("%d%%", q.PercentUsed)
	}
	return strings.Join([]string{
		t.Title().Render(" User quota "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + q.Name),
		muted(t, fmt.Sprintf("  UID %d", q.UID)),
		"",
		muted(t, "  Quota ") + total,
		muted(t, "  Used  ") + HumanBytes(uint64(q.TotalUsed)*1024*1024),
		muted(t, "  %     ") + pct,
		muted(t, fmt.Sprintf("  Volumes %d", len(q.Volumes))),
	}, "\n")
}

func quotaOrUnlimited(mib int64) string {
	if mib <= 0 {
		return "unlimited"
	}
	return HumanBytes(uint64(mib) * 1024 * 1024)
}

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

// AppsPage shows installed packages and system services on one screen.
type AppsPage struct {
	ctx Ctx

	pkgs           []dsm.Package
	svcs           []dsm.Service
	pkgErr, svcErr error

	cursor  int
	filter  Filter
	pending map[string]string
	flash   string

	detailPkg *dsm.Package
	detailSvc *dsm.Service
	confirm   *Confirm
}

func NewAppsPage(c Ctx) tui.View {
	return &AppsPage{
		ctx:     c,
		confirm: NewConfirm(c.Theme),
		pending: map[string]string{},
	}
}

func (a *AppsPage) Name() string                   { return "apps" }
func (a *AppsPage) Title() string                  { return "Apps" }
func (a *AppsPage) Icon() string                   { return "▣" }
func (a *AppsPage) RefreshInterval() time.Duration { return 20 * time.Second }
func (a *AppsPage) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start (pkg)")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop (pkg)")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart (pkg)")),
		key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "uninstall (pkg, confirm)")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable (svc)")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable (svc)")),
	)
}

func (a *AppsPage) Init() tea.Cmd { return tea.Batch(a.fetchPkgs(), a.fetchSvcs()) }

func (a *AppsPage) fetchPkgs() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Package, error) { return c.Packages(ctx) },
		func(p []dsm.Package, err error) tea.Msg { return packagesMsg{P: p, Err: err} },
	)
}

func (a *AppsPage) fetchSvcs() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Service, error) { return c.Services(ctx) },
		func(s []dsm.Service, err error) tea.Msg { return servicesMsg{S: s, Err: err} },
	)
}

// rowKind for the apps page.
type appRowKind int

const (
	appRowPackage appRowKind = iota
	appRowService
)

type appRow struct {
	kind  appRowKind
	index int
}

func (a *AppsPage) filterPkgs() []dsm.Package {
	if a.filter.Value() == "" {
		return a.pkgs
	}
	out := make([]dsm.Package, 0)
	for _, p := range a.pkgs {
		if MatchesAll(a.filter.Value(), p.ID, p.Name, p.Maintainer, p.Status, p.Version) {
			out = append(out, p)
		}
	}
	return out
}

func (a *AppsPage) filterSvcs() []dsm.Service {
	if a.filter.Value() == "" {
		return a.svcs
	}
	out := make([]dsm.Service, 0)
	for _, s := range a.svcs {
		if MatchesAll(a.filter.Value(), s.ID, s.DisplayName(), s.EnableStatus) {
			out = append(out, s)
		}
	}
	return out
}

func (a *AppsPage) flatten() []appRow {
	var out []appRow
	for i := range a.filterPkgs() {
		out = append(out, appRow{appRowPackage, i})
	}
	for i := range a.filterSvcs() {
		out = append(out, appRow{appRowService, i})
	}
	return out
}

func (a *AppsPage) current() (appRow, bool) {
	rows := a.flatten()
	if a.cursor < 0 || a.cursor >= len(rows) {
		return appRow{}, false
	}
	return rows[a.cursor], true
}

func (a *AppsPage) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := a.confirm.Update(msg); handled {
		return a, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "uninstall:"); ok {
			a.pending[rest] = "uninstall"
			a.flash = "uninstalling " + rest + "…"
			c := a.ctx.Client
			return a, tui.Fetch(60*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.PackageUninstall(ctx, rest) },
				func(_ struct{}, err error) tea.Msg { return pkgActionMsg{ID: rest, Action: "uninstall", Err: err} },
			)
		}
	case CancelledMsg:
		a.flash = "cancelled"
		return a, nil
	}

	if a.detailPkg != nil || a.detailSvc != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				a.detailPkg, a.detailSvc = nil, nil
				return a, nil
			}
		}
		return a, nil
	}

	if a.filter.IsActive() {
		if a.filter.Update(msg) {
			return a, nil
		}
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return a, tea.Batch(a.fetchPkgs(), a.fetchSvcs())
	case packagesMsg:
		a.pkgs, a.pkgErr = m.P, m.Err
		a.clampCursor()
	case servicesMsg:
		a.svcs, a.svcErr = m.S, m.Err
		a.clampCursor()
	case pkgActionMsg:
		delete(a.pending, m.ID)
		if m.Err != nil {
			a.flash = m.Action + " failed: " + m.Err.Error()
		} else {
			a.flash = m.Action + " ok"
		}
		return a, a.fetchPkgs()
	case svcActionMsg:
		if m.Err != nil {
			a.flash = m.Action + " failed: " + m.Err.Error()
		} else {
			a.flash = m.Action + " ok"
		}
		return a, a.fetchSvcs()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := a.flatten()
			if a.cursor < len(rows)-1 {
				a.cursor++
			}
		case "k", "up":
			if a.cursor > 0 {
				a.cursor--
			}
		case "g":
			a.cursor = 0
		case "G":
			a.cursor = max(len(a.flatten())-1, 0)
		case "/":
			a.filter.Open()
		case "esc":
			if a.filter.Value() != "" {
				a.filter.Clear()
				a.cursor = 0
			}
		case "r":
			return a, tea.Batch(a.fetchPkgs(), a.fetchSvcs())
		case "enter":
			if r, ok := a.current(); ok {
				switch r.kind {
				case appRowPackage:
					p := a.filterPkgs()[r.index]
					a.detailPkg = &p
				case appRowService:
					sv := a.filterSvcs()[r.index]
					a.detailSvc = &sv
				}
			}
		case "s", "x", "R":
			if r, ok := a.current(); ok && r.kind == appRowPackage {
				id := a.filterPkgs()[r.index].ID
				action := map[string]string{"s": "start", "x": "stop", "R": "restart"}[m.String()]
				a.pending[id] = action
				c := a.ctx.Client
				return a, tui.Fetch(20*time.Second,
					func(ctx context.Context) (struct{}, error) { return struct{}{}, c.PackageControl(ctx, id, action) },
					func(_ struct{}, err error) tea.Msg { return pkgActionMsg{ID: id, Action: action, Err: err} },
				)
			}
		case "U":
			if r, ok := a.current(); ok && r.kind == appRowPackage {
				p := a.filterPkgs()[r.index]
				a.confirm.Ask("uninstall:"+p.ID, "Uninstall "+p.Name+"?",
					"This permanently removes the package and its settings.")
			}
		case "e", "d":
			if r, ok := a.current(); ok && r.kind == appRowService {
				sv := a.filterSvcs()[r.index]
				action := "enable"
				if m.String() == "d" {
					action = "disable"
				}
				c := a.ctx.Client
				id := sv.ID
				return a, tui.Fetch(20*time.Second,
					func(ctx context.Context) (struct{}, error) { return struct{}{}, c.ServiceControl(ctx, id, action) },
					func(_ struct{}, err error) tea.Msg { return svcActionMsg{ID: id, Action: action, Err: err} },
				)
			}
		}
	}
	return a, nil
}

func (a *AppsPage) clampCursor() {
	n := len(a.flatten())
	if a.cursor >= n {
		a.cursor = n - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
}

func (a *AppsPage) Render(width, height int) string {
	t := a.ctx.Theme
	if a.confirm.Open() {
		return a.confirm.Render(width, height)
	}
	if a.detailPkg != nil {
		return renderPackageDetail(t, width, *a.detailPkg)
	}
	if a.detailSvc != nil {
		return renderServiceDetail(t, width, *a.detailSvc)
	}

	pkgs := a.filterPkgs()
	svcs := a.filterSvcs()
	cursorRow := a.cursor

	var parts []string
	idx := 0

	parts = append(parts, sectionHeader(t, width, "Packages", len(pkgs), a.pkgErr))
	if a.pkgs == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(pkgs) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, p := range pkgs {
		parts = append(parts, a.renderPackageRow(p, cursorRow == idx))
		idx++
	}

	parts = append(parts, "")
	parts = append(parts, sectionHeader(t, width, "Services", len(svcs), a.svcErr))
	if a.svcs == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(svcs) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, sv := range svcs {
		parts = append(parts, a.renderServiceRow(sv, cursorRow == idx))
		idx++
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · [s]tart [x]stop [R]estart [U]ninstall (pkg) · [e]nable [d]isable (svc)"))
	if a.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+a.flash))
	}
	if v := a.filter.Render(t); v != "" {
		parts = append(parts, v)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (a *AppsPage) renderPackageRow(p dsm.Package, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := p.Status
	if act, ok := a.pending[p.ID]; ok {
		status = act + "…"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(p.Name), 28), " ",
		padRight(muted.Render(p.Version), 18), " ",
		padRight(muted.Render(p.Maintainer), 24), " ",
		t.HealthStyle(status).Render(status),
	)
}

func (a *AppsPage) renderServiceRow(s dsm.Service, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := s.EnableStatus
	switch state {
	case "enabled":
		state = "enabled"
	case "static":
		state = "always-on"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(s.ID), 30), " ",
		padRight(muted.Render(s.DisplayName()), 28), " ",
		t.HealthStyle(state).Render(state),
	)
}

// sectionHeader renders a "title (n) ───────" header used by every page.
// The rule uses the strong-border accent rather than the soft one so it
// reads as a real divider on dark terminals, not just a barely-visible
// smudge.
func sectionHeader(t tui.Theme, width int, title string, count int, err error) string {
	left := t.Title().Render(title) + " " +
		lipgloss.NewStyle().Foreground(t.Muted).Render(fmt.Sprintf("(%d)", count))
	leftW := lipgloss.Width(left)
	rule := strings.Repeat("─", maxInt(width-leftW-4, 0))
	out := left + "  " + lipgloss.NewStyle().Foreground(t.Accent).Faint(true).Render(rule)
	if err != nil {
		out += "\n" + errLine(t, err)
	}
	return out
}

// caretGlyph renders the row-selected indicator used by every page.
func caretGlyph(t tui.Theme, on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▸")
	}
	return " "
}

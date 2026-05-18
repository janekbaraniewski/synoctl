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

// VMM is the read-only Virtual Machine Manager view. It's tabbed:
//
//   - VMs:   the guests Virtual Machine Manager has registered.
//   - Hosts: the virtualization hosts that run them (just the NAS on a
//     single-box install; multiple entries on a Synology HA cluster).
//
// Modes cycle with `t` or jump directly via `1`/`2`. Each mode owns its
// own cursor + filter so switching tabs preserves the user's place,
// mirroring the Apps view's pattern.
//
// Refresh cadence is 30s — guest status moves on operator timescales,
// not high-frequency telemetry.

// vmmMode is which tab the VMM view is showing.
type vmmMode int

const (
	vmmModeVMs vmmMode = iota
	vmmModeHosts
)

func (m vmmMode) String() string {
	switch m {
	case vmmModeVMs:
		return "VMs"
	case vmmModeHosts:
		return "Hosts"
	}
	return "?"
}

type vmmGuestsMsg struct {
	V   []dsm.VirtualMachine
	Err error
}

type vmmHostsMsg struct {
	H   []dsm.VMHost
	Err error
}

// VMMView is the Virtual Machine Manager tabbed view.
type VMMView struct {
	ctx Ctx

	mode vmmMode

	vms     []dsm.VirtualMachine
	vmsErr  error
	hosts   []dsm.VMHost
	hostErr error

	bases  [2]listBase
	loaded [2]bool

	detailVM   *dsm.VirtualMachine
	detailHost *dsm.VMHost
}

// NewVMM constructs the Virtual Machine Manager view.
func NewVMM(c Ctx) tui.View { return &VMMView{ctx: c} }

func (v *VMMView) Name() string                   { return "vmm" }
func (v *VMMView) Title() string                  { return "Virtual Machines" }
func (v *VMMView) Icon() string                   { return "▩" }
func (v *VMMView) RefreshInterval() time.Duration { return 30 * time.Second }

func (v *VMMView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next mode")),
		key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "vms")),
		key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "hosts")),
	)
}

// Hint returns the mode-aware bottom-bar hint.
func (v *VMMView) Hint() string {
	switch v.mode {
	case vmmModeVMs:
		return "t mode · 1/2 jump · ⏎ details · / filter · r refresh"
	case vmmModeHosts:
		return "t mode · 1/2 jump · ⏎ details · / filter · r refresh"
	}
	return ""
}

func (v *VMMView) Init() tea.Cmd {
	return tea.Batch(v.fetchVMs(), v.fetchHosts())
}

func (v *VMMView) fetchVMs() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.VirtualMachine, error) { return c.VirtualMachines(ctx) },
		func(x []dsm.VirtualMachine, err error) tea.Msg { return vmmGuestsMsg{V: x, Err: err} },
	)
}

func (v *VMMView) fetchHosts() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.VMHost, error) { return c.VMHosts(ctx) },
		func(x []dsm.VMHost, err error) tea.Msg { return vmmHostsMsg{H: x, Err: err} },
	)
}

func (v *VMMView) base() *listBase { return &v.bases[v.mode] }

func (v *VMMView) visibleVMs() []dsm.VirtualMachine {
	if v.bases[vmmModeVMs].FilterValue() == "" {
		return v.vms
	}
	out := make([]dsm.VirtualMachine, 0, len(v.vms))
	for _, g := range v.vms {
		if MatchesAll(v.bases[vmmModeVMs].FilterValue(),
			g.ID, g.Name, g.VMID, g.Status, g.Host, g.Description) {
			out = append(out, g)
		}
	}
	return out
}

func (v *VMMView) visibleHosts() []dsm.VMHost {
	if v.bases[vmmModeHosts].FilterValue() == "" {
		return v.hosts
	}
	out := make([]dsm.VMHost, 0, len(v.hosts))
	for _, h := range v.hosts {
		if MatchesAll(v.bases[vmmModeHosts].FilterValue(), h.ID, h.Name, h.HostIP) {
			out = append(out, h)
		}
	}
	return out
}

func (v *VMMView) visibleCount() int {
	switch v.mode {
	case vmmModeVMs:
		return len(v.visibleVMs())
	case vmmModeHosts:
		return len(v.visibleHosts())
	}
	return 0
}

func (v *VMMView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Detail overlay swallows everything except esc/q.
	if v.detailVM != nil || v.detailHost != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detailVM, v.detailHost = nil, nil
		}
		return v, nil
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchVMs(), v.fetchHosts())
	case vmmGuestsMsg:
		v.vms, v.vmsErr = m.V, m.Err
		v.loaded[vmmModeVMs] = true
		v.bases[vmmModeVMs].ClampCursor(len(v.visibleVMs()))
		return v, nil
	case vmmHostsMsg:
		v.hosts, v.hostErr = m.H, m.Err
		v.loaded[vmmModeHosts] = true
		v.bases[vmmModeHosts].ClampCursor(len(v.visibleHosts()))
		return v, nil
	}

	// Forward to per-mode listBase for cursor + filter.
	if _, handled := v.base().HandleKey(msg, v.visibleCount()); handled {
		return v, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "t":
			v.mode = vmmMode((int(v.mode) + 1) % 2)
			return v, nil
		case "1":
			v.mode = vmmModeVMs
			return v, nil
		case "2":
			v.mode = vmmModeHosts
			return v, nil
		case "r":
			switch v.mode {
			case vmmModeVMs:
				return v, v.fetchVMs()
			case vmmModeHosts:
				return v, v.fetchHosts()
			}
		case "enter":
			v.openDetail()
		}
	}
	return v, nil
}

func (v *VMMView) openDetail() {
	switch v.mode {
	case vmmModeVMs:
		rows := v.visibleVMs()
		if c := v.base().Cursor(); c < len(rows) {
			g := rows[c]
			v.detailVM = &g
		}
	case vmmModeHosts:
		rows := v.visibleHosts()
		if c := v.base().Cursor(); c < len(rows) {
			h := rows[c]
			v.detailHost = &h
		}
	}
}

func (v *VMMView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detailVM != nil {
		return renderVMDetail(t, width, *v.detailVM)
	}
	if v.detailHost != nil {
		return renderVMHostDetail(t, width, *v.detailHost)
	}

	// Empty-state when VMM isn't installed at all.
	if v.loaded[vmmModeVMs] && v.loaded[vmmModeHosts] &&
		len(v.vms) == 0 && len(v.hosts) == 0 &&
		v.vmsErr == nil && v.hostErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"▩  Virtual Machine Manager",
			"Virtual Machine Manager is not installed, or no guests are registered.",
			"Install Virtual Machine Manager from Package Center to see your VMs and hosts here."), height)
	}

	var parts []string
	parts = append(parts, v.renderTabs(width))
	parts = append(parts, "")

	switch v.mode {
	case vmmModeVMs:
		parts = append(parts, v.renderVMs(width)...)
	case vmmModeHosts:
		parts = append(parts, v.renderHosts(width)...)
	}

	if f := v.base().FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *VMMView) renderTabs(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	idle := lipgloss.NewStyle().Foreground(t.Text)
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	tab := func(m vmmMode, count int) string {
		label := m.String() + " (" + itoa(count) + ")"
		if m == v.mode {
			return "▎ " + active.Render(label)
		}
		return "  " + idle.Render(label)
	}
	row := tab(vmmModeVMs, len(v.vms)) + "   " + tab(vmmModeHosts, len(v.hosts))
	rule := mu.Render(strings.Repeat("─", maxInt(width-2, 0)))
	return row + "\n" + rule
}

func (v *VMMView) renderVMs(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleVMs()
	out := []string{sectionHeader(t, width, "Virtual machines", len(rows), v.vmsErr)}
	if !v.loaded[vmmModeVMs] {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	cursor := v.bases[vmmModeVMs].Cursor()
	for i, g := range rows {
		out = append(out, v.renderVMRow(g, i == cursor))
	}
	return out
}

func (v *VMMView) renderHosts(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleHosts()
	out := []string{sectionHeader(t, width, "Virtualization hosts", len(rows), v.hostErr)}
	if !v.loaded[vmmModeHosts] {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	cursor := v.bases[vmmModeHosts].Cursor()
	for i, h := range rows {
		out = append(out, v.renderHostRow(h, i == cursor))
	}
	return out
}

func (v *VMMView) renderVMRow(g dsm.VirtualMachine, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := g.Status
	if status == "" {
		status = "unknown"
	}
	cpuMem := fmt.Sprintf("%d vCPU · %s", g.VCPUNum, HumanBytes(uint64(g.VRAMSize)*1024*1024))
	host := g.Host
	if host == "" {
		host = "—"
	}
	flags := ""
	if g.AutoRun.Bool() {
		flags += "↺ "
	}
	if g.EnableHA.Bool() {
		flags += "HA "
	}
	if flags == "" {
		flags = "—"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(g.Name, 24)), 24), " ",
		padRight(mu.Render(clipTo(cpuMem, 22)), 22), " ",
		padRight(mu.Render(clipTo(host, 14)), 14), " ",
		padRight(mu.Render(flags), 8), " ",
		t.HealthStyle(vmStatusKey(status)).Render(status),
	)
}

func (v *VMMView) renderHostRow(h dsm.VMHost, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := "stopped"
	if h.Running.Bool() {
		state = "running"
	}
	ram := fmt.Sprintf("%s / %s",
		HumanBytes(uint64(h.RAMUsed)*1024*1024),
		HumanBytes(uint64(h.RAMTotal)*1024*1024))
	cpu := fmt.Sprintf("%.1f%%", h.CPUUsage)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(h.Name, 22)), 22), " ",
		padRight(mu.Render(clipTo(h.HostIP, 18)), 18), " ",
		padLeft(mu.Render(cpu), 7), " ",
		padRight(mu.Render(ram), 22), " ",
		padLeft(mu.Render(fmt.Sprintf("%d vm", h.VMCount)), 6), " ",
		t.HealthStyle(state).Render(state),
	)
}

// Inspect implements tui.Inspector for the side preview pane.
func (v *VMMView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	switch v.mode {
	case vmmModeVMs:
		rows := v.visibleVMs()
		if v.bases[vmmModeVMs].Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderVMInspect(t, width, rows[v.bases[vmmModeVMs].Cursor()])
	case vmmModeHosts:
		rows := v.visibleHosts()
		if v.bases[vmmModeHosts].Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderVMHostInspect(t, width, rows[v.bases[vmmModeHosts].Cursor()])
	}
	return ""
}

func renderVMDetail(t tui.Theme, width int, g dsm.VirtualMachine) string {
	if width < 60 {
		width = 60
	}
	status := g.Status
	if status == "" {
		status = "unknown"
	}
	host := g.Host
	if host == "" {
		host = "—"
	}
	parts := []string{
		hero(t, width, "▩", g.Name, status, host),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", g.ID},
			{"VM ID", g.VMID},
			{"Name", g.Name},
			{"Status", status},
			{"Host", host},
			{"vCPU", fmt.Sprintf("%d", g.VCPUNum)},
			{"vRAM", HumanBytes(uint64(g.VRAMSize) * 1024 * 1024)},
			{"Description", g.Description},
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
		chip("auto-run", g.AutoRun.Bool()),
		chip("HA enabled", g.EnableHA.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · read-only — VMM write actions aren't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderVMHostDetail(t tui.Theme, width int, h dsm.VMHost) string {
	if width < 60 {
		width = 60
	}
	state := "stopped"
	if h.Running.Bool() {
		state = "running"
	}
	parts := []string{
		hero(t, width, "▩", h.Name, state, h.HostIP),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", h.ID},
			{"Name", h.Name},
			{"Host IP", h.HostIP},
			{"VM count", fmt.Sprintf("%d", h.VMCount)},
			{"CPU usage", fmt.Sprintf("%.1f%%", h.CPUUsage)},
			{"RAM total", HumanBytes(uint64(h.RAMTotal) * 1024 * 1024)},
			{"RAM used", HumanBytes(uint64(h.RAMUsed) * 1024 * 1024)},
		}),
	}
	parts = append(parts, noteCard(t, width, "  esc to go back"))
	return strings.Join(parts, "\n")
}

func renderVMInspect(t tui.Theme, width int, g dsm.VirtualMachine) string {
	status := g.Status
	if status == "" {
		status = "unknown"
	}
	host := g.Host
	if host == "" {
		host = "—"
	}
	return strings.Join([]string{
		t.Title().Render(" Virtual machine "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(g.Name, width-4)),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + g.VMID),
		"  " + t.HealthStyle(vmStatusKey(status)).Render(status),
		"",
		muted(t, "  Host     ") + clipTo(host, width-14),
		muted(t, "  vCPU     ") + fmt.Sprintf("%d", g.VCPUNum),
		muted(t, "  vRAM     ") + HumanBytes(uint64(g.VRAMSize)*1024*1024),
		muted(t, "  Auto-run ") + yesNo(g.AutoRun.Bool()),
		muted(t, "  HA       ") + yesNo(g.EnableHA.Bool()),
		"",
		muted(t, "  Description"),
		"  " + clipTo(g.Description, width-4),
	}, "\n")
}

func renderVMHostInspect(t tui.Theme, width int, h dsm.VMHost) string {
	_ = width
	state := "stopped"
	if h.Running.Bool() {
		state = "running"
	}
	return strings.Join([]string{
		t.Title().Render(" Virtualization host "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + h.Name),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + h.HostIP),
		"  " + t.HealthStyle(state).Render(state),
		"",
		muted(t, "  VMs       ") + fmt.Sprintf("%d", h.VMCount),
		muted(t, "  CPU usage ") + fmt.Sprintf("%.1f%%", h.CPUUsage),
		muted(t, "  RAM used  ") + HumanBytes(uint64(h.RAMUsed)*1024*1024),
		muted(t, "  RAM total ") + HumanBytes(uint64(h.RAMTotal)*1024*1024),
	}, "\n")
}

// vmStatusKey maps VMM's status vocabulary onto the Theme's HealthStyle
// keys so guest rows colour-code consistently with the rest of the TUI.
func vmStatusKey(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running":
		return "running"
	case "paused", "saved":
		return "warning"
	case "shutoff", "stopped":
		return "stopped"
	case "crashed":
		return "error"
	}
	return s
}

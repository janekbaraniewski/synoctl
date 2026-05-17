package views

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Group bundles related sub-views (each a tui.View) under a single
// top-level tab. It renders its own sub-tab bar above the active
// sub-view, and forwards messages to whichever sub-view is focused.
//
// Use Group whenever a conceptual area is split across several
// concrete screens — e.g. Storage = Volumes / Disks / Shares / Files —
// so the top-level tab bar stays short and discoverable.
type Group struct {
	name     string
	title    string
	icon     string
	subviews []tui.View
	active   int
	// h/l, [, ] cycle between sub-views. The keys are stable so they
	// don't collide with view-local actions.
	prevKey key.Binding
	nextKey key.Binding
}

// NewGroup constructs a top-level group. The first sub-view is active
// on entry.
func NewGroup(name, title, icon string, subs ...tui.View) tui.View {
	return &Group{
		name:     name,
		title:    title,
		icon:     icon,
		subviews: subs,
		// We deliberately do NOT bind [/] here — those are owned by the
		// top-level tab bar in app.go. H/L (and </>) cycle within a
		// group.
		prevKey: key.NewBinding(key.WithKeys("H", "<"), key.WithHelp("H / <", "prev sub-view")),
		nextKey: key.NewBinding(key.WithKeys("L", ">"), key.WithHelp("L / >", "next sub-view")),
	}
}

func (g *Group) Name() string  { return g.name }
func (g *Group) Title() string { return g.title }
func (g *Group) Icon() string  { return g.icon }

// RefreshInterval delegates to whichever sub-view is active so each one
// retains its own polling cadence.
func (g *Group) RefreshInterval() time.Duration {
	if g.active < len(g.subviews) {
		return g.subviews[g.active].RefreshInterval()
	}
	return 0
}

// Bindings expose the group-level navigation keys plus the active
// sub-view's bindings.
func (g *Group) Bindings() []key.Binding {
	out := []key.Binding{g.prevKey, g.nextKey}
	if g.active < len(g.subviews) {
		out = append(out, g.subviews[g.active].Bindings()...)
	}
	return out
}

// Init kicks off the active sub-view.
func (g *Group) Init() tea.Cmd {
	if len(g.subviews) == 0 {
		return nil
	}
	return g.subviews[g.active].Init()
}

func (g *Group) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, g.nextKey):
			g.active = (g.active + 1) % len(g.subviews)
			return g, g.subviews[g.active].Init()
		case key.Matches(km, g.prevKey):
			g.active = (g.active - 1 + len(g.subviews)) % len(g.subviews)
			return g, g.subviews[g.active].Init()
		}
	}
	// Forward to active sub-view.
	nv, cmd := g.subviews[g.active].Update(msg)
	g.subviews[g.active] = nv
	return g, cmd
}

// Render lays the sub-tab bar over the active sub-view's render.
func (g *Group) Render(width, height int) string {
	bar := g.renderSubBar(width)
	body := g.subviews[g.active].Render(width, height-1)
	return bar + "\n" + body
}

func (g *Group) renderSubBar(width int) string {
	// We don't have a theme reference here — but the lipgloss styles we
	// apply pull from terminal-default colors plus a single accent, so a
	// muted bar that's only an aesthetic delimiter is fine.
	mauve := lipgloss.Color("#cba6f7")
	muted := lipgloss.Color("#a6adc8")
	bg := lipgloss.Color("#181825")

	var chips []string
	for i, sv := range g.subviews {
		label := " " + sv.Icon() + "  " + sv.Title() + " "
		var s lipgloss.Style
		if i == g.active {
			s = lipgloss.NewStyle().Foreground(lipgloss.Color("#181825")).Background(mauve).Bold(true)
		} else {
			s = lipgloss.NewStyle().Foreground(muted).Background(bg)
		}
		chips = append(chips, s.Render(label))
	}
	row := strings.Join(chips, "")
	pad := width - lipgloss.Width(row)
	if pad > 0 {
		row += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
	}
	return row
}

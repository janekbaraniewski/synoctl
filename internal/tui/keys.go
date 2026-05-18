package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is the global keymap. Each view also exposes view-local bindings
// surfaced in the help overlay.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Enter    key.Binding
	Back     key.Binding
	Refresh  key.Binding
	Filter   key.Binding
	Palette  key.Binding
	Help     key.Binding
	Quit     key.Binding

	// Sidebar nav — n/p step views (skipping headers), N/P jump sections.
	// Tab / Shift+Tab stay as a power-user fallback.
	NavNext     key.Binding
	NavPrev     key.Binding
	NavSection  key.Binding // 'N' — next section
	NavSectionP key.Binding // 'P' — previous section
	NavFocus    key.Binding // ctrl-l toggle sidebar focus (palette substitute)

	Action       key.Binding // 'a' — context action menu
	ToggleInsp   key.Binding // 'i' — toggle inspector
	ToggleSide   key.Binding // 'b' — toggle sidebar (more space for content)
	YankPath     key.Binding // 'y' — copy current path/id to clipboard
}

// DefaultKeys returns the standard, vim-flavoured binding set.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("PgUp", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("PgDn", "page down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "select")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Palette:  key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		NavNext:     key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("⇥", "next view")),
		NavPrev:     key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("⇧⇥", "prev view")),
		NavSection:  key.NewBinding(key.WithKeys("}"), key.WithHelp("}", "next section")),
		NavSectionP: key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "prev section")),
		NavFocus:    key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("^l", "focus sidebar")),

		Action:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "actions")),
		ToggleInsp: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "toggle inspector")),
		ToggleSide: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("^b", "toggle sidebar")),
		YankPath:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank id")),
	}
}

package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
)

// ViewContext is what each view receives at construction time. It carries
// shared services so views never reach into globals.
type ViewContext struct {
	Client *dsm.Client
	Theme  Theme
	Keys   KeyMap
	Logger *log.Logger
}

// TextEditing is an optional interface a View can implement to tell the
// shell that it currently owns text input (an open form, prompt, OTP
// modal, …). When true, the shell suppresses global key bindings so
// typed runes — q, a, /, r, etc. — reach the view instead of triggering
// quit / action menu / refresh. Views without typed-input state should
// not implement this interface.
type TextEditing interface {
	IsTextEditing() bool
}

// Hinter is an optional interface a View can implement to render its own
// context-aware bottom hint bar. The string is shown verbatim in the
// shell's hint strip (which then suffixes globals like : / ? / q).
//
// Returning an empty string falls back to a static global hint. Each view
// is expected to keep the string short — one line, ≤ width-30 cols.
type Hinter interface {
	Hint() string
}

// View is the contract every navigable screen implements.
//
// Refresh:
//
//	When RefreshInterval returns a non-zero duration, the root app sends a
//	Tick message to the view at that cadence (paused when the view is
//	hidden). Views consume the tick by issuing a fetch command.
type View interface {
	Name() string  // stable id, e.g. "dashboard"
	Title() string // human label for tabs/header
	Icon() string  // one-rune label (nerd-font glyph or ASCII fallback)

	Init() tea.Cmd
	Update(msg tea.Msg) (View, tea.Cmd)
	Render(width, height int) string

	// Bindings returns view-local key bindings (shown in `?` help).
	Bindings() []key.Binding

	// RefreshInterval is the desired polling cadence. Return 0 to disable.
	RefreshInterval() time.Duration
}

// TickMsg is delivered to a view on its refresh cadence.
type TickMsg struct {
	View string    // recipient name
	At   time.Time // when the tick fired
}

// scheduleTick returns a tea.Cmd that emits TickMsg after d for view name.
func scheduleTick(name string, d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return TickMsg{View: name, At: t}
	})
}

// Fetch wraps an arbitrary blocking call so it can be issued as a tea.Cmd.
// On completion it emits a typed message produced by mk.
func Fetch[T any](timeout time.Duration, fn func(ctx context.Context) (T, error), mk func(T, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v, err := fn(ctx)
		return mk(v, err)
	}
}

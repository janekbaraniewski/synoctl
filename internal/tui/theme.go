// Package tui hosts the bubbletea application — root model, theme, keymap,
// and the registry of views.
//
// The theme is a deliberately distinctive dark-first palette: deep slate
// surfaces with a warm amber signature accent, paired with a cool cyan
// secondary. It is *not* a Catppuccin lift — `synoctl` is supposed to feel
// like its own tool when you launch it, not "yet another charm app". The
// light variant mirrors with appropriate lightness flips so terminals on
// light backgrounds remain readable.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the full visual palette used by every view. Views consume
// semantic slots (Accent, Success, Warn …) rather than raw hex so view
// code never makes its own colour choices.
type Theme struct {
	// Surfaces
	Bg         lipgloss.AdaptiveColor // page background
	BgAlt      lipgloss.AdaptiveColor // alternate row / sub-card / chrome strip
	Surface    lipgloss.AdaptiveColor // card body
	SurfaceAlt lipgloss.AdaptiveColor // inspector pane bg (subtly different)
	Border     lipgloss.AdaptiveColor // soft divider
	BorderHi   lipgloss.AdaptiveColor // strong divider / focused border

	// Text
	Text  lipgloss.AdaptiveColor // primary text
	Muted lipgloss.AdaptiveColor // secondary text
	Faint lipgloss.AdaptiveColor // tertiary / hint text

	// Semantic accents
	Accent  lipgloss.AdaptiveColor // brand / primary — signature amber
	Accent2 lipgloss.AdaptiveColor // secondary accent — cool cyan
	Success lipgloss.AdaptiveColor
	Warn    lipgloss.AdaptiveColor
	Error   lipgloss.AdaptiveColor
	Info    lipgloss.AdaptiveColor

	// Charts — gradient stops, low → high
	GradLo  lipgloss.AdaptiveColor
	GradMid lipgloss.AdaptiveColor
	GradHi  lipgloss.AdaptiveColor
}

// DefaultTheme returns the signature palette.
//
// Design notes:
//   - Background and chrome use a slate-blue family so the foreground colour
//     can carry the brand without fighting the surfaces.
//   - The amber accent is the visual identity — it appears on the brand
//     wordmark, the active sidebar marker, the focused border, and the
//     primary gauge ramp's high stop is *not* this accent (so a "100% CPU"
//     gauge doesn't look like brand chrome).
//   - GradHi is deliberately red rather than amber so danger reads at a
//     glance even on terminals that mute warm hues.
func DefaultTheme() Theme {
	return Theme{
		Bg:         lipgloss.AdaptiveColor{Light: "#f3f4f7", Dark: "#0d1117"},
		BgAlt:      lipgloss.AdaptiveColor{Light: "#e7eaf0", Dark: "#161b22"},
		Surface:    lipgloss.AdaptiveColor{Light: "#dde1ea", Dark: "#1c2230"},
		SurfaceAlt: lipgloss.AdaptiveColor{Light: "#e7eaf0", Dark: "#171d28"},
		Border:     lipgloss.AdaptiveColor{Light: "#c0c5d2", Dark: "#2a3142"},
		BorderHi:   lipgloss.AdaptiveColor{Light: "#c2761b", Dark: "#f0a020"},

		Text:  lipgloss.AdaptiveColor{Light: "#1d2230", Dark: "#e6e9ef"},
		Muted: lipgloss.AdaptiveColor{Light: "#5b6275", Dark: "#8b94a7"},
		Faint: lipgloss.AdaptiveColor{Light: "#8b94a7", Dark: "#555c6e"},

		Accent:  lipgloss.AdaptiveColor{Light: "#c2761b", Dark: "#f0a020"}, // signature amber
		Accent2: lipgloss.AdaptiveColor{Light: "#1f6f9e", Dark: "#5dade2"}, // cool cyan
		Success: lipgloss.AdaptiveColor{Light: "#3a8a4f", Dark: "#6fb86f"},
		Warn:    lipgloss.AdaptiveColor{Light: "#a37a16", Dark: "#f0c674"},
		Error:   lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#e26a6a"},
		Info:    lipgloss.AdaptiveColor{Light: "#1f6f9e", Dark: "#5dade2"},

		GradLo:  lipgloss.AdaptiveColor{Light: "#3a8a4f", Dark: "#6fb86f"}, // green
		GradMid: lipgloss.AdaptiveColor{Light: "#a37a16", Dark: "#f0c674"}, // amber
		GradHi:  lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#e26a6a"}, // red
	}
}

// Card returns a standard rounded-border card style. Pass focused=true to
// highlight the border in the signature amber.
func (t Theme) Card(focused bool) lipgloss.Style {
	border := t.Border
	if focused {
		border = t.BorderHi
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
}

// Title returns the style for a card title row.
func (t Theme) Title() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true).
		Padding(0, 1)
}

// Chip is a compact rounded label used for status pills and keybinding hints.
func (t Theme) Chip(c lipgloss.AdaptiveColor) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.Bg).
		Background(c).
		Padding(0, 1).
		Bold(true)
}

// SubtleChip is a low-contrast version of Chip for hint bars.
func (t Theme) SubtleChip() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.Muted).
		Background(t.BgAlt).
		Padding(0, 1)
}

// HeaderRow is the highlighted row used at the top of tables.
func (t Theme) HeaderRow() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)
}

// Row is the default table row style.
func (t Theme) Row() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Text)
}

// RowAlt is the alternate (zebra) row style.
func (t Theme) RowAlt() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Text).Background(t.BgAlt)
}

// Selected highlights the active row. Uses an amber underline on the alt
// background instead of inverting the row — inversion fights inline status
// chips that ride on row text.
func (t Theme) Selected() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.Accent).
		Background(t.BgAlt).
		Bold(true)
}

// HealthStyle returns a foreground-coloured style for a normalized health
// string. Backgrounds are intentionally avoided here — they look like ugly
// horizontal blocks when used inside tables.
func (t Theme) HealthStyle(s string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "normal", "ok", "running", "connected", "healthy", "active", "enabled", "up":
		return lipgloss.NewStyle().Foreground(t.Success).Bold(true)
	case "warn", "warning", "degrade", "rebuilding", "starting", "stopping":
		return lipgloss.NewStyle().Foreground(t.Warn).Bold(true)
	case "crashed", "error", "err", "stop", "stopped", "disconnected", "broken", "failed", "down", "disabled":
		return lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(t.Muted)
	}
}

// Wordmark renders the signature brand bug used in the top bar. Two letter
// pairs flanked by a copper marker glyph that stays put as the rest of the
// row changes.
func (t Theme) Wordmark() string {
	mark := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("◣")
	word := lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render("synoctl")
	return mark + " " + word
}

// SidebarMarker is the active-row indicator used in the left nav. A thin
// vertical bar in the signature amber.
func (t Theme) SidebarMarker(active bool) string {
	if active {
		return lipgloss.NewStyle().Foreground(t.Accent).Render("▎")
	}
	return lipgloss.NewStyle().Foreground(t.BgAlt).Render(" ")
}

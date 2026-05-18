// Package views contains the TUI views. Each view registers itself in
// internal/tui by exporting a constructor that returns a tui.View.
package views

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Ctx is a re-export so views don't need to import tui directly.
type Ctx = tui.ViewContext

// ParseSizeString accepts DSM's stringy byte counts (e.g. "16104808448")
// and returns a number. Empty/invalid → 0.
func ParseSizeString(s string) uint64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// HumanBytes formats a byte count with IEC suffixes (KiB/MiB/…).
func HumanBytes(n uint64) string {
	return humanize.IBytes(n)
}

// HumanRate formats bytes/second.
func HumanRate(n int64) string {
	if n < 0 {
		n = 0
	}
	return humanize.IBytes(uint64(n)) + "/s"
}

// Gauge renders a fixed-width gradient progress bar. ratio is 0..1.
// The bar shifts colour from GradLo → GradMid → GradHi as it fills.
func Gauge(theme tui.Theme, width int, ratio float64) string {
	if width < 4 {
		width = 4
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(float64(width) * ratio)

	// Pick colour from ratio.
	var c lipgloss.AdaptiveColor
	switch {
	case ratio < 0.6:
		c = theme.GradLo
	case ratio < 0.85:
		c = theme.GradMid
	default:
		c = theme.GradHi
	}

	full := strings.Repeat("█", filled)
	empty := strings.Repeat("░", width-filled)
	return lipgloss.NewStyle().Foreground(c).Render(full) +
		lipgloss.NewStyle().Foreground(theme.Faint).Render(empty)
}

// Sparkline renders a 1-row line of block characters reflecting `data`.
// Width is the number of columns; data is sampled from the tail.
func Sparkline(theme tui.Theme, width int, data []float64) string {
	if width <= 0 || len(data) == 0 {
		return strings.Repeat("·", maxInt(width, 0))
	}
	// Sample last `width` values.
	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	d := data[start:]

	// Normalize.
	var max float64
	for _, v := range d {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		max = 1
	}
	const ramp = " ▁▂▃▄▅▆▇█"
	rampRunes := []rune(ramp)
	var b strings.Builder
	for _, v := range d {
		idx := int((v / max) * float64(len(rampRunes)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(rampRunes) {
			idx = len(rampRunes) - 1
		}
		b.WriteRune(rampRunes[idx])
	}
	// Pad if data shorter than width.
	if pad := width - b.Len(); pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	return lipgloss.NewStyle().Foreground(theme.Accent2).Render(b.String())
}

// Card draws a titled card filling the requested width.
func Card(theme tui.Theme, width int, title, body string, focused bool) string {
	if width < 12 {
		width = 12
	}
	titleStyle := theme.Title()
	bodyStyle := lipgloss.NewStyle().Foreground(theme.Text)
	content := titleStyle.Render(title) + "\n" + bodyStyle.Render(body)
	style := theme.Card(focused).Width(width - 2) // account for border
	return style.Render(content)
}

// Pad centers s in a line of given width.
func Pad(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	left := (width - w) / 2
	right := width - w - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// Wrap returns s clipped to width on a single line.
func Wrap(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	// Naive ANSI-unsafe trim; acceptable for our simple inline strings.
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// HumanDurationFromDSMUptime parses DSM's uptime string into a Duration.
// DSM 7 returns "hhhh:mm:ss" (no days component), DSM 6 returns
// "ddd:hh:mm:ss". We accept either.
func HumanDurationFromDSMUptime(s string) time.Duration {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
	case 4:
		d, _ := strconv.Atoi(parts[0])
		h, _ := strconv.Atoi(parts[1])
		m, _ := strconv.Atoi(parts[2])
		sec, _ := strconv.Atoi(parts[3])
		return time.Duration(d)*24*time.Hour + time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// errLine renders an indented red error line for use inside a card body.
func errLine(t tui.Theme, err error) string {
	return lipgloss.NewStyle().Foreground(t.Error).Render("  " + err.Error())
}

// fitOrScroll trims output to at most `n` lines, padding short output to
// fill. Used by list views whose composed sections might exceed the
// allocated body height.
func fitOrScroll(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s + strings.Repeat("\n", n-len(lines))
	}
	return strings.Join(lines[:n], "\n")
}

// sectionHeader renders a "Title (n) ───────" header used by list views.
// The accent-coloured rule uses the soft border so it reads as a real
// divider on dark terminals without overpowering the title.
func sectionHeader(t tui.Theme, width int, title string, count int, err error) string {
	titleStyle := t.Title().Render(title)
	countStyle := lipgloss.NewStyle().Foreground(t.Muted).Render("(" + itoaShort(count) + ")")
	left := titleStyle + " " + countStyle
	leftW := lipgloss.Width(left)
	rule := strings.Repeat("─", maxInt(width-leftW-4, 0))
	out := left + "  " + lipgloss.NewStyle().Foreground(t.Accent).Faint(true).Render(rule)
	if err != nil {
		out += "\n" + errLine(t, err)
	}
	return out
}

// caretGlyph renders the ▸ row-selected indicator. When off it returns a
// single space of the same visual width so columns don't shift.
func caretGlyph(t tui.Theme, on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▸")
	}
	return " "
}

func itoaShort(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := [20]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = '0' + byte(n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

package views

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Column is one column in a rendered table.
type Column struct {
	Header string
	Width  int // 0 → flex, distributes remaining width evenly
	Align  lipgloss.Position
}

// Cell is a single rendered cell — content plus an optional style override.
// Use a zero Cell to mean "just text, default style".
type Cell struct {
	Text     string
	Style    lipgloss.Style
	HasStyle bool
}

// Plain wraps text into a Cell with no style override.
func Plain(s string) Cell { return Cell{Text: s} }

// Styled wraps text into a Cell with a style override (e.g. a chip).
func Styled(s string, st lipgloss.Style) Cell { return Cell{Text: s, Style: st, HasStyle: true} }

// Table renders a complete table widget for use inside a card. It draws:
//   - one header row in the accent colour
//   - zebra-striped body rows
//   - the row at `selected` highlighted (-1 to disable)
//
// Width is the total interior width available; columns with Width=0 split
// the remaining space evenly after the explicit widths are subtracted.
func Table(theme tui.Theme, width, height int, cols []Column, rows [][]Cell, selected int) string {
	widths := computeWidths(cols, width)

	var b strings.Builder
	// Header
	b.WriteString(renderRow(theme.HeaderRow(), cols, widths, headerCells(cols)))
	b.WriteByte('\n')
	b.WriteString(lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", width)))
	b.WriteByte('\n')

	// Body. We deliberately avoid zebra striping: when the table contains
	// per-cell chip styles (status, level …) the alternating-row
	// background fights the chip background and ends up looking like
	// ugly horizontal bars. A single calm foreground for unselected rows
	// plus a strong highlight on the selected row reads much cleaner.
	maxRows := height - 3
	if maxRows < 1 {
		maxRows = 1
	}
	visibleStart := 0
	if selected >= 0 && selected >= maxRows {
		visibleStart = selected - maxRows + 1
	}
	end := visibleStart + maxRows
	if end > len(rows) {
		end = len(rows)
	}
	for i := visibleStart; i < end; i++ {
		style := theme.Row()
		if i == selected {
			style = theme.Selected()
		}
		b.WriteString(renderRow(style, cols, widths, rows[i]))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	// Pad to height with blank lines so the card doesn't change size.
	rendered := b.String()
	got := strings.Count(rendered, "\n") + 1
	target := height
	if got < target {
		rendered += strings.Repeat("\n", target-got)
	}
	return rendered
}

func headerCells(cols []Column) []Cell {
	out := make([]Cell, len(cols))
	for i, c := range cols {
		out[i] = Plain(c.Header)
	}
	return out
}

func renderRow(base lipgloss.Style, cols []Column, widths []int, cells []Cell) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		text := ""
		var cellStyle lipgloss.Style
		hasStyle := false
		if i < len(cells) {
			text = cells[i].Text
			cellStyle = cells[i].Style
			hasStyle = cells[i].HasStyle
		}
		w := widths[i]
		text = clipTo(text, w)
		var rendered string
		if hasStyle {
			rendered = cellStyle.Render(text)
		} else {
			rendered = text
		}
		// Pad to width respecting alignment, after styling so the visual
		// width is correct.
		visW := lipgloss.Width(rendered)
		pad := w - visW
		if pad < 0 {
			pad = 0
		}
		switch c.Align {
		case lipgloss.Right:
			rendered = strings.Repeat(" ", pad) + rendered
		case lipgloss.Center:
			l := pad / 2
			r := pad - l
			rendered = strings.Repeat(" ", l) + rendered + strings.Repeat(" ", r)
		default:
			rendered = rendered + strings.Repeat(" ", pad)
		}
		parts[i] = rendered
	}
	return base.Render(strings.Join(parts, " "))
}

func computeWidths(cols []Column, total int) []int {
	widths := make([]int, len(cols))
	used := len(cols) - 1 // single-space gaps
	flex := 0
	for i, c := range cols {
		widths[i] = c.Width
		if c.Width == 0 {
			flex++
		} else {
			used += c.Width
		}
	}
	remain := total - used
	if remain < flex {
		remain = flex
	}
	if flex > 0 {
		per := remain / flex
		extra := remain - per*flex
		for i, c := range cols {
			if c.Width == 0 {
				widths[i] = per
				if extra > 0 {
					widths[i]++
					extra--
				}
			}
		}
	}
	return widths
}

func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

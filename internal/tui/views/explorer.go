package views

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Explorer is the universal "call any SYNO.* API" surface. It loads the
// SYNO.API.Info table, lets the user pick an API + method + version,
// edit free-form key=value params, then renders the JSON response with
// theme-aware syntax highlighting.
//
// This view exists so synoctl is a true safety net: even when a
// feature isn't wrapped in a typed helper, the underlying DSM call is
// still reachable from the TUI.
type Explorer struct {
	ctx Ctx

	// Data
	apis    []dsm.APIEntry
	apisErr error
	loaded  bool

	// UI state
	list      listBase
	focus     explorerFocus
	selected  *dsm.APIEntry // nil until the user enters into a row
	method    textinput.Model
	version   textinput.Model
	params    []paramRow
	paramIdx  int  // focused row inside the params editor
	hasResult bool // true once a call has returned (success or error)
	resultRaw json.RawMessage
	resultErr error
	resultAt  time.Time
	pending   bool
	flash     string

	// Scrolling
	resultScroll int
}

type explorerFocus int

const (
	focusList explorerFocus = iota
	focusMethod
	focusVersion
	focusParams
	focusResult
)

// paramRow is one editable row in the params editor. Each row has its
// own key/value textinputs so cursor positions are preserved as the
// user moves between rows.
type paramRow struct {
	key   textinput.Model
	value textinput.Model
	// sub == 0 means the key is focused, sub == 1 means the value is.
	sub int
}

func newParamRow() paramRow {
	k := textinput.New()
	k.Prompt = ""
	k.CharLimit = 64
	k.Placeholder = "key"
	v := textinput.New()
	v.Prompt = ""
	v.CharLimit = 512
	v.Placeholder = "value"
	return paramRow{key: k, value: v}
}

// NewExplorer constructs the API Explorer view.
func NewExplorer(c Ctx) tui.View {
	m := textinput.New()
	m.Prompt = ""
	m.CharLimit = 64
	m.Placeholder = "list"
	m.SetValue("list")

	v := textinput.New()
	v.Prompt = ""
	v.CharLimit = 6
	v.Placeholder = "1"

	return &Explorer{
		ctx:     c,
		method:  m,
		version: v,
		focus:   focusList,
	}
}

func (e *Explorer) Name() string                   { return "explorer" }
func (e *Explorer) Title() string                  { return "API Explorer" }
func (e *Explorer) Icon() string                   { return "⚙" }
func (e *Explorer) RefreshInterval() time.Duration { return 0 }

func (e *Explorer) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter list")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "select API / invoke (params pane)")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥", "next field")),
		key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧⇥", "previous field")),
		key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "add param row")),
		key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "remove focused param row")),
		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("⌃r", "invoke")),
		key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "scroll result down")),
		key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "scroll result up")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / clear filter / close result")),
	}
}

// ─────────────────────────── fetching ───────────────────────────

type explorerAPIsMsg struct {
	APIs []dsm.APIEntry
	Err  error
}

type explorerResultMsg struct {
	Data json.RawMessage
	Err  error
	At   time.Time
}

func (e *Explorer) Init() tea.Cmd { return e.fetchAPIs() }

func (e *Explorer) fetchAPIs() tea.Cmd {
	c := e.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.APIEntry, error) {
			if err := c.Info(ctx); err != nil {
				return nil, err
			}
			return c.APIList(), nil
		},
		func(v []dsm.APIEntry, err error) tea.Msg { return explorerAPIsMsg{APIs: v, Err: err} },
	)
}

func (e *Explorer) invoke() tea.Cmd {
	if e.selected == nil {
		return nil
	}
	c := e.ctx.Client
	if c == nil {
		return nil
	}
	api := e.selected.Name
	method := strings.TrimSpace(e.method.Value())
	if method == "" {
		method = "list"
	}
	verStr := strings.TrimSpace(e.version.Value())
	ver := e.selected.Max
	if verStr != "" {
		if n, err := strconv.Atoi(verStr); err == nil && n > 0 {
			ver = n
		}
	}
	params := url.Values{}
	for _, p := range e.params {
		k := strings.TrimSpace(p.key.Value())
		if k == "" {
			continue
		}
		params.Set(k, p.value.Value())
	}
	e.pending = true
	e.flash = fmt.Sprintf("calling %s.%s (v%d)…", api, method, ver)
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (json.RawMessage, error) {
			return c.CallRaw(ctx, api, ver, method, params)
		},
		func(d json.RawMessage, err error) tea.Msg {
			return explorerResultMsg{Data: d, Err: err, At: time.Now()}
		},
	)
}

// ─────────────────────────── update ───────────────────────────

func (e *Explorer) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case explorerAPIsMsg:
		e.apis, e.apisErr = m.APIs, m.Err
		e.loaded = true
		e.list.ClampCursor(len(e.filteredAPIs()))
		return e, nil
	case explorerResultMsg:
		e.pending = false
		e.hasResult = true
		e.resultRaw, e.resultErr, e.resultAt = m.Data, m.Err, m.At
		e.resultScroll = 0
		e.focus = focusResult
		if m.Err != nil {
			e.flash = "error: " + m.Err.Error()
		} else {
			e.flash = "ok"
		}
		return e, nil
	}

	// When focus is on the list, defer to listBase's filter/cursor handling.
	if e.focus == focusList {
		// The list's filter editor must own runes when active.
		rowCount := len(e.filteredAPIs())
		if cmd, handled := e.list.HandleKey(msg, rowCount); handled {
			return e, cmd
		}
	}

	km, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return e, nil
	}

	// Global key handling for the view (focus-aware).
	switch km.String() {
	case "tab":
		e.cycleFocus(1)
		e.applyFocus()
		return e, nil
	case "shift+tab":
		e.cycleFocus(-1)
		e.applyFocus()
		return e, nil
	case "ctrl+r":
		if e.selected != nil {
			return e, e.invoke()
		}
		return e, nil
	case "esc":
		// Close the result viewer, otherwise step focus back to the list.
		if e.focus == focusResult {
			e.focus = focusParams
			e.applyFocus()
			return e, nil
		}
		if e.focus != focusList {
			e.focus = focusList
			e.applyFocus()
			return e, nil
		}
		// At list focus, clear the selection so the user is back at
		// nothing-selected state.
		if e.selected != nil {
			e.selected = nil
			e.params = nil
			e.hasResult = false
			e.resultRaw = nil
			e.resultErr = nil
			e.flash = ""
		}
		return e, nil
	case "J":
		// Only intercept J/K when no text-input owns focus, so users can
		// still type capital letters into method/version/param fields.
		if e.focus == focusList || e.focus == focusResult {
			e.resultScroll++
			return e, nil
		}
	case "K":
		if e.focus == focusList || e.focus == focusResult {
			if e.resultScroll > 0 {
				e.resultScroll--
			}
			return e, nil
		}
	}

	switch e.focus {
	case focusList:
		// Enter into the call panel for the highlighted API.
		if km.Type == tea.KeyEnter {
			rows := e.filteredAPIs()
			if c := e.list.Cursor(); c >= 0 && c < len(rows) {
				sel := rows[c]
				e.selected = &sel
				e.version.SetValue(strconv.Itoa(sel.Max))
				if e.method.Value() == "" {
					e.method.SetValue("list")
				}
				e.hasResult = false
				e.resultErr = nil
				e.resultRaw = nil
				e.flash = ""
				e.focus = focusMethod
				e.applyFocus()
			}
			return e, nil
		}
	case focusMethod:
		var cmd tea.Cmd
		e.method, cmd = e.method.Update(msg)
		return e, cmd
	case focusVersion:
		var cmd tea.Cmd
		e.version, cmd = e.version.Update(msg)
		return e, cmd
	case focusParams:
		return e, e.updateParams(km)
	case focusResult:
		// `enter` re-runs the call; everything else is consumed silently.
		if km.Type == tea.KeyEnter {
			return e, e.invoke()
		}
	}
	return e, nil
}

func (e *Explorer) cycleFocus(delta int) {
	// Skip focusList until we have a selection (you can't fall back to
	// list-only navigation without losing the call context); skip
	// focusResult until a call has been issued.
	order := []explorerFocus{focusList}
	if e.selected != nil {
		order = append(order, focusMethod, focusVersion, focusParams)
		if e.hasResult {
			order = append(order, focusResult)
		}
	}
	idx := 0
	for i, f := range order {
		if f == e.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(order)) % len(order)
	e.focus = order[idx]
}

// applyFocus blurs/focuses textinputs to match e.focus.
func (e *Explorer) applyFocus() {
	e.method.Blur()
	e.version.Blur()
	for i := range e.params {
		e.params[i].key.Blur()
		e.params[i].value.Blur()
	}
	switch e.focus {
	case focusMethod:
		e.method.Focus()
	case focusVersion:
		e.version.Focus()
	case focusParams:
		if e.paramIdx >= 0 && e.paramIdx < len(e.params) {
			if e.params[e.paramIdx].sub == 0 {
				e.params[e.paramIdx].key.Focus()
			} else {
				e.params[e.paramIdx].value.Focus()
			}
		}
	}
}

// updateParams handles keypresses while the params editor is focused.
func (e *Explorer) updateParams(km tea.KeyMsg) tea.Cmd {
	switch km.String() {
	case "+":
		e.params = append(e.params, newParamRow())
		e.paramIdx = len(e.params) - 1
		e.params[e.paramIdx].sub = 0
		e.applyFocus()
		return nil
	case "-":
		if e.paramIdx >= 0 && e.paramIdx < len(e.params) {
			e.params = append(e.params[:e.paramIdx], e.params[e.paramIdx+1:]...)
			if e.paramIdx >= len(e.params) {
				e.paramIdx = len(e.params) - 1
			}
			e.applyFocus()
		}
		return nil
	case "up":
		if e.paramIdx > 0 {
			e.paramIdx--
			e.applyFocus()
		}
		return nil
	case "down":
		if e.paramIdx < len(e.params)-1 {
			e.paramIdx++
			e.applyFocus()
		}
		return nil
	case "left":
		if e.paramIdx >= 0 && e.paramIdx < len(e.params) && e.params[e.paramIdx].sub == 1 {
			e.params[e.paramIdx].sub = 0
			e.applyFocus()
			return nil
		}
		// Fall through to textinput's left arrow for in-field cursor move.
	case "right":
		if e.paramIdx >= 0 && e.paramIdx < len(e.params) && e.params[e.paramIdx].sub == 0 {
			e.params[e.paramIdx].sub = 1
			e.applyFocus()
			return nil
		}
		// Fall through.
	}
	if km.Type == tea.KeyEnter {
		// Enter from the params editor issues the call.
		return e.invoke()
	}
	if e.paramIdx >= 0 && e.paramIdx < len(e.params) {
		var cmd tea.Cmd
		if e.params[e.paramIdx].sub == 0 {
			e.params[e.paramIdx].key, cmd = e.params[e.paramIdx].key.Update(km)
		} else {
			e.params[e.paramIdx].value, cmd = e.params[e.paramIdx].value.Update(km)
		}
		return cmd
	}
	return nil
}

// ─────────────────────────── filtering ───────────────────────────

func (e *Explorer) filteredAPIs() []dsm.APIEntry {
	if e.list.FilterValue() == "" {
		return e.apis
	}
	out := make([]dsm.APIEntry, 0, len(e.apis))
	for _, a := range e.apis {
		if e.list.FilterMatch(a.Name, a.Path) {
			out = append(out, a)
		}
	}
	return out
}

// ─────────────────────────── render ───────────────────────────

func (e *Explorer) Render(width, height int) string {
	t := e.ctx.Theme

	leftW := width / 2
	if leftW < 36 {
		leftW = 36
	}
	if leftW > width-30 {
		leftW = width - 30
	}
	rightW := width - leftW

	left := e.renderList(leftW, height-2)
	right := e.renderRight(rightW, height-2)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ select / invoke · ⇥ next field · + add param · - remove param · ⌃r invoke · J/K scroll result · / filter · esc back")
	if e.flash != "" {
		color := t.Muted
		if e.resultErr != nil && e.hasResult {
			color = t.Error
		} else if e.hasResult {
			color = t.Success
		} else if e.pending {
			color = t.Accent2
		}
		footer = lipgloss.NewStyle().Foreground(color).Render("  " + e.flash)
	}
	if fv := e.list.FilterFooter(t); fv != "" {
		footer = footer + " " + fv
	}
	return fitOrScroll(body+"\n"+footer, height)
}

func (e *Explorer) renderList(width, height int) string {
	t := e.ctx.Theme
	focused := e.focus == focusList
	apis := e.filteredAPIs()

	title := t.Title().Render(fmt.Sprintf(" APIs (%d) ", len(apis)))
	if !e.loaded {
		body := title + "\n  " + muted(t, "loading SYNO.API.Info…")
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}
	if e.apisErr != nil {
		body := title + "\n" + errLine(t, e.apisErr)
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}
	if len(apis) == 0 {
		body := title + "\n  " + muted(t, "(none)")
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}

	// Visible window — keep the cursor on screen.
	innerH := height - 4
	if innerH < 4 {
		innerH = 4
	}
	cursor := e.list.Cursor()
	start := 0
	if cursor >= innerH {
		start = cursor - innerH + 1
	}
	end := start + innerH
	if end > len(apis) {
		end = len(apis)
	}

	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, e.renderAPIRow(width-4, apis[i], i == cursor))
	}
	body := title + "\n" + strings.Join(lines, "\n")
	return t.Card(focused).Width(width - 2).Height(height).Render(body)
}

func (e *Explorer) renderAPIRow(width int, a dsm.APIEntry, highlight bool) string {
	t := e.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	ver := fmt.Sprintf("v%d→v%d", a.Min, a.Max)
	// Layout: caret  name(flex)  ver(12)
	nameW := width - 2 - 13
	if nameW < 16 {
		nameW = 16
	}
	name := text.Render(clipTo(a.Name, nameW))
	path := muted.Render(clipTo("  "+a.Path, nameW))
	verS := lipgloss.NewStyle().Foreground(t.Accent2).Render(ver)
	first := lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(name, nameW), " ",
		padLeft(verS, 12),
	)
	second := "  " + padRight(path, nameW)
	return first + "\n" + second
}

func (e *Explorer) renderRight(width, height int) string {
	t := e.ctx.Theme
	if e.selected == nil {
		body := t.Title().Render(" Call panel ") + "\n  " +
			muted(t, "select an API on the left (⏎) to compose a call.")
		return t.Card(false).Width(width - 2).Height(height).Render(body)
	}

	// Top: descriptor card.
	descr := e.renderDescriptor(width)
	// Middle: editor (method, version, params).
	editor := e.renderEditor(width)
	// Bottom: result viewer (always rendered so the layout doesn't jump).
	usedLines := lipgloss.Height(descr) + lipgloss.Height(editor)
	resultH := height - usedLines - 1
	if resultH < 6 {
		resultH = 6
	}
	result := e.renderResult(width, resultH)
	return strings.Join([]string{descr, editor, result}, "\n")
}

func (e *Explorer) renderDescriptor(width int) string {
	t := e.ctx.Theme
	a := *e.selected
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	pair := func(k, v string) string {
		return muted.Render(k+":") + " " + text.Render(v)
	}
	title := t.Title().Render(" " + a.Name + " ")
	row := strings.Join([]string{
		pair("path", a.Path),
		pair("min", strconv.Itoa(a.Min)),
		pair("max", strconv.Itoa(a.Max)),
	}, "   ")
	return t.Card(false).Width(width - 2).Render(title + "\n" + row)
}

func (e *Explorer) renderEditor(width int) string {
	t := e.ctx.Theme
	focused := e.focus == focusMethod || e.focus == focusVersion || e.focus == focusParams

	title := t.Title().Render(" Call ")
	label := func(s string, on bool) string {
		st := lipgloss.NewStyle().Foreground(t.Muted)
		if on {
			st = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
		}
		return st.Render(s)
	}
	e.method.Width = 24
	e.version.Width = 8
	methodLine := label("method", e.focus == focusMethod) + "  " +
		e.fieldBox(e.method.View(), e.focus == focusMethod, 26)
	verLine := label("version", e.focus == focusVersion) + "  " +
		e.fieldBox(e.version.View(), e.focus == focusVersion, 10)

	// Params section.
	pHeader := label("params", e.focus == focusParams) + "  " +
		lipgloss.NewStyle().Foreground(t.Faint).Render("(+ add · - remove · ←/→ key↔value · ⏎ invoke)")

	var paramLines []string
	if len(e.params) == 0 {
		paramLines = append(paramLines,
			"  "+lipgloss.NewStyle().Foreground(t.Faint).Render("no params · press + to add a row"))
	} else {
		for i, p := range e.params {
			rowFocused := e.focus == focusParams && i == e.paramIdx
			p.key.Width = 18
			p.value.Width = width - 38
			if p.value.Width < 12 {
				p.value.Width = 12
			}
			caret := caretGlyph(t, rowFocused)
			kBox := e.fieldBox(p.key.View(), rowFocused && p.sub == 0, 20)
			vBox := e.fieldBox(p.value.View(), rowFocused && p.sub == 1, width-38)
			line := lipgloss.JoinHorizontal(lipgloss.Center,
				caret, " ", kBox, "  =  ", vBox,
			)
			paramLines = append(paramLines, line)
		}
	}

	body := title + "\n" + methodLine + "\n" + verLine + "\n\n" +
		pHeader + "\n" + strings.Join(paramLines, "\n")
	return t.Card(focused).Width(width - 2).Render(body)
}

// fieldBox draws a thin underline under the rendered text-input view,
// using the accent border when focused.
func (e *Explorer) fieldBox(view string, focused bool, width int) string {
	t := e.ctx.Theme
	color := t.Border
	if focused {
		color = t.Accent
	}
	w := lipgloss.Width(view)
	if w < width {
		view = view + strings.Repeat(" ", width-w)
	}
	underline := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("─", width))
	return view + "\n" + underline
}

func (e *Explorer) renderResult(width, height int) string {
	t := e.ctx.Theme
	focused := e.focus == focusResult
	title := t.Title().Render(" Response ")
	header := title
	if !e.resultAt.IsZero() {
		ts := lipgloss.NewStyle().Foreground(t.Muted).Render(
			"  at " + e.resultAt.Format("15:04:05"))
		header += ts
	}

	if e.pending {
		body := header + "\n  " + lipgloss.NewStyle().Foreground(t.Accent2).Render("invoking…")
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}
	if !e.hasResult {
		body := header + "\n  " +
			muted(t, "press ⏎ from the params pane or ⌃r to invoke")
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}
	if e.resultErr != nil {
		// Surface the typed dsm.Error code via its formatted message.
		body := header + "\n" +
			lipgloss.NewStyle().Foreground(t.Error).Bold(true).Render("  "+e.resultErr.Error())
		return t.Card(focused).Width(width - 2).Height(height).Render(body)
	}

	// Pretty-print and highlight.
	pretty := prettifyJSON(e.resultRaw)
	lines := strings.Split(pretty, "\n")
	innerH := height - 3
	if innerH < 1 {
		innerH = 1
	}
	if e.resultScroll > len(lines)-1 {
		e.resultScroll = max(len(lines)-1, 0)
	}
	if e.resultScroll < 0 {
		e.resultScroll = 0
	}
	end := e.resultScroll + innerH
	if end > len(lines) {
		end = len(lines)
	}
	view := strings.Join(lines[e.resultScroll:end], "\n")
	view = highlightJSON(t, view, width-6)

	scrollHint := ""
	if len(lines) > innerH {
		scrollHint = lipgloss.NewStyle().Foreground(t.Faint).Render(
			fmt.Sprintf("  J/K to scroll · %d-%d / %d", e.resultScroll+1, end, len(lines)))
	}
	body := header + "\n" + view
	if scrollHint != "" {
		body += "\n" + scrollHint
	}
	return t.Card(focused).Width(width - 2).Height(height).Render(body)
}

// ─────────────────────────── JSON helpers ───────────────────────────

func prettifyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	var any interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		// Hand the raw bytes back — better than throwing away the body.
		return string(raw)
	}
	b, err := json.MarshalIndent(any, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// highlightJSON walks the pretty-printed text and applies theme tokens
// to keys, strings, numbers, and the literals true/false/null. It works
// line-by-line so it can be safely composed with the scroll window.
func highlightJSON(t tui.Theme, s string, _ int) string {
	keyStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	strStyle := lipgloss.NewStyle().Foreground(t.Success)
	numStyle := lipgloss.NewStyle().Foreground(t.Accent2)
	literalStyle := lipgloss.NewStyle().Foreground(t.Warn)
	punctStyle := lipgloss.NewStyle().Foreground(t.Muted)
	errStyle := lipgloss.NewStyle().Foreground(t.Error).Bold(true)

	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, highlightJSONLine(line, keyStyle, strStyle, numStyle, literalStyle, punctStyle, errStyle))
	}
	return strings.Join(out, "\n")
}

func highlightJSONLine(line string, keyStyle, strStyle, numStyle, literalStyle, punctStyle, errStyle lipgloss.Style) string {
	// A tiny hand-rolled scanner. JSON pretty-printed by MarshalIndent
	// uses very regular formatting — keys are always followed by `: `,
	// so we can split on the first unquoted `:` per line.
	//
	// We avoid importing a json scanner / regexp because the input is
	// trusted (we produced it via MarshalIndent) and we only want
	// theme-aware colouring, not validation.

	// Preserve leading whitespace.
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	leading := line[:i]
	rest := line[i:]
	if rest == "" {
		return line
	}

	var b strings.Builder
	b.WriteString(leading)

	j := 0
	for j < len(rest) {
		c := rest[j]
		switch {
		case c == '"':
			// Scan to closing quote (no escape inside — Marshal escapes
			// embedded quotes as \").
			start := j
			j++
			for j < len(rest) {
				if rest[j] == '\\' && j+1 < len(rest) {
					j += 2
					continue
				}
				if rest[j] == '"' {
					j++
					break
				}
				j++
			}
			token := rest[start:j]
			// Decide key vs string: keys are followed by `:` (maybe with whitespace).
			k := j
			for k < len(rest) && rest[k] == ' ' {
				k++
			}
			if k < len(rest) && rest[k] == ':' {
				b.WriteString(keyStyle.Render(token))
			} else {
				b.WriteString(strStyle.Render(token))
			}
		case c == ':' || c == ',' || c == '{' || c == '}' || c == '[' || c == ']':
			b.WriteString(punctStyle.Render(string(c)))
			j++
		case c == ' ':
			b.WriteByte(c)
			j++
		case (c >= '0' && c <= '9') || c == '-':
			start := j
			for j < len(rest) {
				cc := rest[j]
				if (cc >= '0' && cc <= '9') || cc == '.' || cc == '-' || cc == '+' || cc == 'e' || cc == 'E' {
					j++
					continue
				}
				break
			}
			b.WriteString(numStyle.Render(rest[start:j]))
		case c == 't' || c == 'f' || c == 'n':
			// true/false/null
			start := j
			for j < len(rest) && rest[j] >= 'a' && rest[j] <= 'z' {
				j++
			}
			tok := rest[start:j]
			switch tok {
			case "true", "false", "null":
				b.WriteString(literalStyle.Render(tok))
			default:
				b.WriteString(errStyle.Render(tok))
			}
		default:
			b.WriteByte(c)
			j++
		}
	}
	return b.String()
}

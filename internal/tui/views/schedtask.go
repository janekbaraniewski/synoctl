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

// SchedTasksView lists DSM's Task Scheduler entries (scripts, S.M.A.R.T.
// tests, reboots…). 60s refresh — schedule changes are rare and we don't
// need to follow them tick-by-tick. The view exposes three mutations
// (`X` run-now, `e` enable, `d` disable) layered on top of the original
// browse experience; mutations route through a Confirm modal for run-now
// (irreversible side-effects) and fire directly for the enable/disable
// toggles (cheap and undoable).

type schedTasksMsg struct {
	T   []dsm.ScheduledTask
	Err error
}

// schedTaskActionMsg carries the result of a run / enable / disable call
// back into the bubbletea loop so the view can flash success or failure
// and trigger a refetch. Action is the verb used in flash messages —
// "run", "enable", "disable" — so the user sees the right word.
type schedTaskActionMsg struct {
	Action string
	Name   string
	Err    error
}

type SchedTasksView struct {
	ctx Ctx

	tasks    []dsm.ScheduledTask
	tasksErr error

	cursor int
	filter Filter
	loaded bool

	detail  *dsm.ScheduledTask
	confirm *Confirm
	flash   string
}

func NewSchedTasks(c Ctx) tui.View {
	return &SchedTasksView{
		ctx:     c,
		confirm: NewConfirm(c.Theme),
	}
}

func (v *SchedTasksView) Name() string                   { return "tasks" }
func (v *SchedTasksView) Title() string                  { return "Scheduled Tasks" }
func (v *SchedTasksView) Icon() string                   { return "◷" }
func (v *SchedTasksView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *SchedTasksView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "run now (confirm)")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
	)
}

// IsTextEditing tells the shell to defer global keybindings while the
// run-now confirm modal owns input. Without it the y/n keys would
// double-fire as global accelerators.
func (v *SchedTasksView) IsTextEditing() bool {
	return v.confirm.Open() || v.filter.IsActive()
}

// Hint is the context-aware bottom-strip text. When the cursor is parked
// on a task row the strip advertises the mutation keys; otherwise it
// stays generic.
func (v *SchedTasksView) Hint() string {
	rows := v.filtered()
	if v.cursor >= 0 && v.cursor < len(rows) {
		return "⏎ details · X run · e enable · d disable · / filter · r refresh"
	}
	return "⏎ details · / filter · r refresh"
}

func (v *SchedTasksView) Init() tea.Cmd { return v.fetch() }

func (v *SchedTasksView) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.ScheduledTask, error) { return c.ScheduledTasks(ctx) },
		func(x []dsm.ScheduledTask, err error) tea.Msg { return schedTasksMsg{T: x, Err: err} },
	)
}

// runCmd kicks off the chosen task and routes the outcome back as a
// schedTaskActionMsg. The 30s ceiling is enough for DSM to accept the
// call — the task itself runs out of band.
func (v *SchedTasksView) runCmd(id int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) { return struct{}{}, c.RunScheduledTask(ctx, id) },
		func(_ struct{}, err error) tea.Msg {
			return schedTaskActionMsg{Action: "run", Name: name, Err: err}
		},
	)
}

// setEnabledCmd routes an enable/disable call back as a
// schedTaskActionMsg. The Action verb on the message drives the
// success/failure flash so the user sees the right word.
func (v *SchedTasksView) setEnabledCmd(id int, name string, enabled bool) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.SetScheduledTaskEnabled(ctx, id, enabled)
		},
		func(_ struct{}, err error) tea.Msg {
			return schedTaskActionMsg{Action: verb, Name: name, Err: err}
		},
	)
}

func (v *SchedTasksView) filtered() []dsm.ScheduledTask {
	if v.filter.Value() == "" {
		return v.tasks
	}
	out := make([]dsm.ScheduledTask, 0, len(v.tasks))
	for _, t := range v.tasks {
		if MatchesAll(v.filter.Value(), t.Name, t.Type, t.Owner, t.Repeat, t.Action, t.LastRunResult) {
			out = append(out, t)
		}
	}
	return out
}

func (v *SchedTasksView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Modal routing first — the confirm overlay owns input while open
	// (run-now is irreversible from a user's perspective, so we don't
	// want a stray `y` keypress to slip past the modal).
	if handled, cmd := v.confirm.Update(msg); handled {
		return v, cmd
	}

	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "schedtask.run:"); ok {
			// Token shape is "schedtask.run:<id>/<name>". Splitting at
			// the first slash keeps task names with slashes intact.
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 {
				var id int
				_, _ = fmt.Sscanf(parts[0], "%d", &id)
				name := parts[1]
				v.flash = "running " + name + "…"
				return v, v.runCmd(id, name)
			}
		}
		return v, nil
	case CancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case schedTaskActionMsg:
		if m.Err != nil {
			v.flash = m.Action + " " + m.Name + " failed: " + m.Err.Error()
		} else {
			switch m.Action {
			case "run":
				v.flash = "started " + m.Name
			case "enable":
				v.flash = m.Name + " enabled"
			case "disable":
				v.flash = m.Name + " disabled"
			default:
				v.flash = m.Action + " " + m.Name + " ok"
			}
		}
		return v, v.fetch()
	}

	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
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
		return v, v.fetch()
	case schedTasksMsg:
		v.tasks, v.tasksErr = m.T, m.Err
		v.loaded = true
		v.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.filtered())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, v.fetch()
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.detail = &t
			}
		case "X":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.confirm.Ask(
					fmt.Sprintf("schedtask.run:%d/%s", t.ID, t.Name),
					"Run task now: "+t.Name+"?",
					"This kicks off the task immediately, outside its schedule. Side effects depend on what the task does (script, S.M.A.R.T. test, reboot…).")
			}
		case "e":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.flash = "enabling " + t.Name + "…"
				return v, v.setEnabledCmd(t.ID, t.Name, true)
			}
		case "d":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.flash = "disabling " + t.Name + "…"
				return v, v.setEnabledCmd(t.ID, t.Name, false)
			}
		}
	}
	return v, nil
}

func (v *SchedTasksView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *SchedTasksView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.confirm.Open() {
		return v.confirm.Render(width, height)
	}
	if v.detail != nil {
		return renderSchedTaskDetail(t, width, *v.detail)
	}

	if v.loaded && len(v.tasks) == 0 && v.tasksErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"◷  Task Scheduler",
			"No scheduled tasks returned by DSM.",
			"Add a task in Control Panel → Task Scheduler to see it here."), height)
	}

	tasks := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "Scheduled tasks", len(tasks), v.tasksErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(tasks) == 0 {
		parts = append(parts, "  "+muted(t, "(none matching)"))
	}
	for i, tk := range tasks {
		parts = append(parts, v.renderRow(tk, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · X run · e enable · d disable · / filter · r refresh"))
	if v.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+v.flash))
	}
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *SchedTasksView) renderRow(tk dsm.ScheduledTask, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := "disabled"
	if tk.Enable.Bool() {
		state = "enabled"
	}
	next := "—"
	if tk.NextTriggerTime > 0 {
		next = time.Unix(tk.NextTriggerTime, 0).Format("2006-01-02 15:04")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(tk.Name, 28)), 28), " ",
		padRight(mu.Render(tk.Type), 12), " ",
		padRight(mu.Render(next), 18), " ",
		padRight(mu.Render(tk.Owner), 14), " ",
		t.HealthStyle(state).Render(state),
	)
}

// Inspect implements tui.Inspector — scheduled tasks carry just enough
// detail (repeat schedule, last result) to benefit from a side pane
// without forcing the user into the full overlay.
func (v *SchedTasksView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	tasks := v.filtered()
	if v.cursor < 0 || v.cursor >= len(tasks) {
		return muted(t, "  (no selection)")
	}
	return renderSchedTaskInspect(t, width, tasks[v.cursor])
}

func renderSchedTaskDetail(t tui.Theme, width int, tk dsm.ScheduledTask) string {
	if width < 60 {
		width = 60
	}
	state := "disabled"
	if tk.Enable.Bool() {
		state = "enabled"
	}
	next, last := "—", "—"
	if tk.NextTriggerTime > 0 {
		next = time.Unix(tk.NextTriggerTime, 0).Format("2006-01-02 15:04")
	}
	if tk.LastRunTime > 0 {
		last = time.Unix(tk.LastRunTime, 0).Format("2006-01-02 15:04")
	}
	repeatTime := "—"
	if tk.RepeatHour > 0 || tk.RepeatMin > 0 {
		repeatTime = fmt.Sprintf("%02d:%02d", tk.RepeatHour, tk.RepeatMin)
	}
	parts := []string{
		hero(t, width, "◷", tk.Name, state, tk.Type),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", fmt.Sprintf("%d", tk.ID)},
			{"Name", tk.Name},
			{"Type", tk.Type},
			{"Owner", tk.Owner},
			{"Owner UID", fmt.Sprintf("%d", tk.OwnerUID)},
			{"Repeat", tk.Repeat},
			{"Time", repeatTime},
			{"Next trigger", next},
			{"Last run", last},
			{"Last result", tk.LastRunResult},
			{"Action", tk.Action},
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
		chip("enabled", tk.Enable.Bool()),
		chip("runnable", tk.CanRun.Bool()),
		chip("editable", tk.CanEdit.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · X run · e enable · d disable"))
	return strings.Join(parts, "\n")
}

func renderSchedTaskInspect(t tui.Theme, width int, tk dsm.ScheduledTask) string {
	_ = width
	state := "disabled"
	if tk.Enable.Bool() {
		state = "enabled"
	}
	next, last := "—", "—"
	if tk.NextTriggerTime > 0 {
		next = time.Unix(tk.NextTriggerTime, 0).Format("2006-01-02 15:04")
	}
	if tk.LastRunTime > 0 {
		last = time.Unix(tk.LastRunTime, 0).Format("2006-01-02 15:04")
	}
	return strings.Join([]string{
		t.Title().Render(" Scheduled task "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + tk.Name),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + tk.Type),
		"  " + t.HealthStyle(state).Render(state),
		"",
		muted(t, "  Owner       ") + tk.Owner,
		muted(t, "  Repeat      ") + tk.Repeat,
		muted(t, "  Next        ") + next,
		muted(t, "  Last run    ") + last,
		muted(t, "  Last result ") + tk.LastRunResult,
		"",
		muted(t, "  Action      ") + clipTo(tk.Action, 38),
	}, "\n")
}

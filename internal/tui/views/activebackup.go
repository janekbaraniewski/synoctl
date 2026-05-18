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

// ActiveBackupView lists Active Backup for Business tasks with their
// latest snapshot. We fetch the most recent ABVersion lazily — only
// when the cursor lands on a row — to avoid an O(tasks) call burst on
// every refresh.
//
// Write actions: `X` runs the task immediately (SYNO.ActiveBackup.Task
// `backup`), `C` cancels the running task (SYNO.ActiveBackup.Task
// `cancel`). Both go through a confirm modal; ABB doesn't have a
// suspend/resume pair (unlike Hyper Backup), so cancel is the only
// stop-style action we expose.

type abTasksMsg struct {
	T   []dsm.ABTask
	Err error
}
type abVersionsMsg struct {
	TaskID int
	V      []dsm.ABVersion
	Err    error
}

// abActionMsg carries the outcome of an Active Backup write back to
// Update(). Kind is "run" / "cancel" — matches the confirm-modal token
// prefix.
type abActionMsg struct {
	Kind   string
	TaskID int
	Name   string
	Err    error
}

type ActiveBackupView struct {
	ctx Ctx

	tasks    []dsm.ABTask
	tasksErr error

	// Latest version per task. Populated lazily.
	latest map[int]*dsm.ABVersion

	cursor int
	filter Filter
	loaded bool

	detail *dsm.ABTask

	confirm *Confirm
	flash   string
}

func NewActiveBackup(c Ctx) tui.View {
	return &ActiveBackupView{
		ctx:     c,
		latest:  map[int]*dsm.ABVersion{},
		confirm: NewConfirm(c.Theme),
	}
}

func (v *ActiveBackupView) Name() string                   { return "activebackup" }
func (v *ActiveBackupView) Title() string                  { return "Active Backup" }
func (v *ActiveBackupView) Icon() string                   { return "⬇" }
func (v *ActiveBackupView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *ActiveBackupView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "run now")),
		key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "cancel")),
	)
}

// Hint feeds the global hint strip. Mirrors the keys Update() handles.
func (v *ActiveBackupView) Hint() string {
	return "⏎ details · X run · C cancel · / filter · r refresh"
}

// IsTextEditing defers global keys (q quit, /, etc.) to the confirm
// modal while it's open — y/n must reach the modal, not trigger the
// global quit handler.
func (v *ActiveBackupView) IsTextEditing() bool { return v.confirm.Open() || v.filter.IsActive() }

func (v *ActiveBackupView) Init() tea.Cmd { return v.fetchTasks() }

func (v *ActiveBackupView) fetchTasks() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.ABTask, error) { return c.ABTasks(ctx) },
		func(x []dsm.ABTask, err error) tea.Msg { return abTasksMsg{T: x, Err: err} },
	)
}

func (v *ActiveBackupView) fetchVersions(taskID int) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.ABVersion, error) { return c.ABVersions(ctx, taskID) },
		func(x []dsm.ABVersion, err error) tea.Msg { return abVersionsMsg{TaskID: taskID, V: x, Err: err} },
	)
}

func (v *ActiveBackupView) runCmd(taskID int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.RunABTask(ctx, taskID)
		},
		func(_ struct{}, err error) tea.Msg {
			return abActionMsg{Kind: "run", TaskID: taskID, Name: name, Err: err}
		},
	)
}

func (v *ActiveBackupView) cancelCmd(taskID int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.CancelABTask(ctx, taskID)
		},
		func(_ struct{}, err error) tea.Msg {
			return abActionMsg{Kind: "cancel", TaskID: taskID, Name: name, Err: err}
		},
	)
}

func (v *ActiveBackupView) filtered() []dsm.ABTask {
	if v.filter.Value() == "" {
		return v.tasks
	}
	out := make([]dsm.ABTask, 0, len(v.tasks))
	for _, t := range v.tasks {
		if MatchesAll(v.filter.Value(), t.Name, t.DeviceType, t.DeviceName, t.RepoPath, t.Status, t.State, t.LastResult) {
			out = append(out, t)
		}
	}
	return out
}

func (v *ActiveBackupView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Confirm modal claims input first.
	if handled, cmd := v.confirm.Update(msg); handled {
		return v, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		// Token shape: "<kind>:<taskID>:<name>" — same routing as
		// HyperBackupView, see splitToken / splitTaskToken there.
		if kind, rest, ok := splitToken(m.Token, ':'); ok {
			id, name := splitTaskToken(rest)
			switch kind {
			case "run":
				v.flash = fmt.Sprintf("running %s…", name)
				return v, v.runCmd(id, name)
			case "cancel":
				v.flash = fmt.Sprintf("cancelling %s…", name)
				return v, v.cancelCmd(id, name)
			}
		}
	case CancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case abActionMsg:
		if m.Err != nil {
			v.flash = fmt.Sprintf("%s failed: %s", m.Kind, m.Err.Error())
		} else {
			switch m.Kind {
			case "run":
				v.flash = fmt.Sprintf("%s started", m.Name)
			case "cancel":
				v.flash = fmt.Sprintf("%s cancelled", m.Name)
			}
		}
		return v, v.fetchTasks()
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
		return v, v.fetchTasks()
	case abTasksMsg:
		v.tasks, v.tasksErr = m.T, m.Err
		v.loaded = true
		v.clampCursor()
		// Kick off a version fetch for whatever's now under the cursor.
		if t, ok := v.currentTask(); ok {
			return v, v.fetchVersions(t.TaskID)
		}
	case abVersionsMsg:
		if m.Err == nil && len(m.V) > 0 {
			latest := m.V[0]
			for _, ver := range m.V {
				if ver.StartTime > latest.StartTime {
					latest = ver
				}
			}
			v.latest[m.TaskID] = &latest
		} else if m.Err == nil {
			// Cache the empty result so we don't keep refetching.
			v.latest[m.TaskID] = &dsm.ABVersion{TaskID: m.TaskID}
		}
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
				if t, ok := v.currentTask(); ok {
					if _, cached := v.latest[t.TaskID]; !cached {
						return v, v.fetchVersions(t.TaskID)
					}
				}
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				if t, ok := v.currentTask(); ok {
					if _, cached := v.latest[t.TaskID]; !cached {
						return v, v.fetchVersions(t.TaskID)
					}
				}
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
			return v, v.fetchTasks()
		case "enter":
			if tk, ok := v.currentTask(); ok {
				v.detail = &tk
			}
		case "X":
			if tk, ok := v.currentTask(); ok {
				v.confirm.Ask(
					fmt.Sprintf("run:%d:%s", tk.TaskID, tk.Name),
					fmt.Sprintf("Run Active Backup task %s now?", tk.Name),
					"It may use significant bandwidth and load the source for the duration of the backup.")
			}
		case "C":
			if tk, ok := v.currentTask(); ok {
				v.confirm.Ask(
					fmt.Sprintf("cancel:%d:%s", tk.TaskID, tk.Name),
					fmt.Sprintf("Cancel Active Backup task %s?", tk.Name),
					"Stops the in-flight backup. The task remains scheduled.")
			}
		}
	}
	return v, nil
}

func (v *ActiveBackupView) currentTask() (dsm.ABTask, bool) {
	rows := v.filtered()
	if v.cursor < 0 || v.cursor >= len(rows) {
		return dsm.ABTask{}, false
	}
	return rows[v.cursor], true
}

func (v *ActiveBackupView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *ActiveBackupView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.confirm.Open() {
		return v.confirm.Render(width, height)
	}
	if v.detail != nil {
		return renderABTaskDetail(t, width, *v.detail, v.latest[v.detail.TaskID])
	}

	if v.loaded && len(v.tasks) == 0 && v.tasksErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⬇  Active Backup for Business",
			"Active Backup for Business is not installed, or no tasks have been configured.",
			"Install Active Backup for Business and configure a source to see protection state here."), height)
	}

	tasks := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "Tasks", len(tasks), v.tasksErr))
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
		"  ↑/↓ move · ⏎ details · X run · C cancel · / filter · esc clear · r refresh"))
	if v.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+v.flash))
	}
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *ActiveBackupView) renderRow(tk dsm.ABTask, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	lastRun := "—"
	if tk.LastBackupTime > 0 {
		lastRun = time.Unix(tk.LastBackupTime, 0).Format("2006-01-02 15:04")
	}
	status := tk.LastResult
	if status == "" {
		status = tk.Status
	}
	if status == "" {
		status = tk.State
	}
	if status == "" {
		status = "—"
	}
	dev := tk.DeviceType
	if tk.DeviceName != "" {
		dev = clipTo(tk.DeviceName, 22)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(tk.Name), 22), " ",
		padRight(mu.Render(padRight(tk.DeviceType, 10)), 10), " ",
		padRight(mu.Render(clipTo(dev, 22)), 22), " ",
		padRight(mu.Render(lastRun), 18), " ",
		t.HealthStyle(status).Render(status),
	)
}

func renderABTaskDetail(t tui.Theme, width int, tk dsm.ABTask, latest *dsm.ABVersion) string {
	if width < 60 {
		width = 60
	}
	status := tk.LastResult
	if status == "" {
		status = tk.Status
	}
	if status == "" {
		status = "unknown"
	}
	lastRun, nextRun := "—", "—"
	if tk.LastBackupTime > 0 {
		lastRun = time.Unix(tk.LastBackupTime, 0).Format("2006-01-02 15:04")
	}
	if tk.NextBackupTime > 0 {
		nextRun = time.Unix(tk.NextBackupTime, 0).Format("2006-01-02 15:04")
	}
	parts := []string{
		hero(t, width, "⬇", tk.Name, status, tk.DeviceType),
		propsCard(t, width, " Task ", [][2]string{
			{"Task ID", fmt.Sprintf("%d", tk.TaskID)},
			{"Name", tk.Name},
			{"Source type", tk.DeviceType},
			{"Device", tk.DeviceName},
			{"Repo ID", fmt.Sprintf("%d", tk.RepoID)},
			{"Repo path", tk.RepoPath},
			{"Schedule", tk.Schedule},
			{"Last backup", lastRun},
			{"Next backup", nextRun},
			{"Total size", HumanBytes(uint64(tk.TotalSize))},
			{"Used size", HumanBytes(uint64(tk.UsedSize))},
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
	}))
	if latest != nil && latest.VersionID > 0 {
		start, end := "—", "—"
		if latest.StartTime > 0 {
			start = time.Unix(latest.StartTime, 0).Format("2006-01-02 15:04")
		}
		if latest.EndTime > 0 {
			end = time.Unix(latest.EndTime, 0).Format("2006-01-02 15:04")
		}
		dur := "—"
		if latest.Duration > 0 {
			dur = (time.Duration(latest.Duration) * time.Second).String()
		}
		parts = append(parts, propsCard(t, width, " Latest version ", [][2]string{
			{"Version", fmt.Sprintf("%d", latest.VersionID)},
			{"Status", latest.Status},
			{"Result", latest.Result},
			{"Started", start},
			{"Ended", end},
			{"Duration", dur},
			{"Used size", HumanBytes(uint64(latest.UsedSize))},
			{"Transferred", HumanBytes(uint64(latest.TransferSize))},
			{"Locked", yesNo(latest.Locked.Bool())},
			{"Note", latest.Note},
		}))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · X run · C cancel"))
	return strings.Join(parts, "\n")
}

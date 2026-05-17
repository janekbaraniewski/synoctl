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

// System bundles together "everything about the box" that doesn't fit
// the other tabs — model/firmware/uptime/temperature plus power
// management (reboot, shutdown). The confirm modal is hosted inside
// the view so the destructive actions stay inside the same modal scope.
type System struct {
	ctx     Ctx
	info    *dsm.SystemInfo
	util    *dsm.Utilization
	infoErr error
	utilErr error
	confirm *Confirm
	flash   string
}

func NewSystem(c Ctx) tui.View {
	return &System{ctx: c, confirm: NewConfirm(c.Theme)}
}

func (s *System) Name() string                   { return "system" }
func (s *System) Title() string                  { return "System" }
func (s *System) Icon() string                   { return "⌂" }
func (s *System) RefreshInterval() time.Duration { return 10 * time.Second }
func (s *System) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "reboot (with confirm)")),
		key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "shutdown (with confirm)")),
	}
}

type sysViewInfoMsg struct {
	I   *dsm.SystemInfo
	Err error
}
type sysViewUtilMsg struct {
	U   *dsm.Utilization
	Err error
}
type sysActionMsg struct {
	Action string
	Err    error
}

func (s *System) Init() tea.Cmd {
	return tea.Batch(s.fetchInfo(), s.fetchUtil())
}

func (s *System) fetchInfo() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.SystemInfo, error) { return c.SystemInfo(ctx) },
		func(i *dsm.SystemInfo, err error) tea.Msg { return sysViewInfoMsg{I: i, Err: err} },
	)
}

func (s *System) fetchUtil() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Second,
		func(ctx context.Context) (*dsm.Utilization, error) { return c.Utilization(ctx) },
		func(u *dsm.Utilization, err error) tea.Msg { return sysViewUtilMsg{U: u, Err: err} },
	)
}

func (s *System) issue(action string) tea.Cmd {
	c := s.ctx.Client
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			switch action {
			case "reboot":
				return struct{}{}, c.Reboot(ctx)
			case "shutdown":
				return struct{}{}, c.Shutdown(ctx)
			}
			return struct{}{}, nil
		},
		func(_ struct{}, err error) tea.Msg { return sysActionMsg{Action: action, Err: err} },
	)
}

func (s *System) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := s.confirm.Update(msg); handled {
		return s, cmd
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return s, tea.Batch(s.fetchInfo(), s.fetchUtil())
	case sysViewInfoMsg:
		s.info, s.infoErr = m.I, m.Err
		return s, nil
	case sysViewUtilMsg:
		s.util, s.utilErr = m.U, m.Err
		return s, nil
	case ConfirmedMsg:
		switch m.Token {
		case "reboot":
			s.flash = "issuing reboot…"
			return s, s.issue("reboot")
		case "shutdown":
			s.flash = "issuing shutdown…"
			return s, s.issue("shutdown")
		}
	case CancelledMsg:
		s.flash = "cancelled " + m.Token
		return s, nil
	case sysActionMsg:
		if m.Err != nil {
			s.flash = m.Action + " failed: " + m.Err.Error()
		} else {
			s.flash = m.Action + " accepted — the box should obey shortly"
		}
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "r":
			return s, tea.Batch(s.fetchInfo(), s.fetchUtil())
		case "B":
			s.confirm.Ask("reboot", "Reboot deep-thought?",
				"The NAS will be unreachable for a few minutes. All running services will be stopped cleanly.")
			return s, nil
		case "S":
			s.confirm.Ask("shutdown", "Shut down deep-thought?",
				"The NAS will power off. You'll need physical access (or Wake-on-LAN) to bring it back up.")
			return s, nil
		}
	}
	return s, nil
}

func (s *System) Render(width, height int) string {
	t := s.ctx.Theme
	if s.info == nil && s.infoErr == nil {
		return Card(t, width, " ⌂  System ", "\n  Loading…\n", true)
	}
	var infoCards []string
	infoCards = append(infoCards, s.renderIdentityCard(width))
	infoCards = append(infoCards, s.renderRuntimeCard(width))
	infoCards = append(infoCards, s.renderActionsCard(width))
	body := strings.Join(infoCards, "\n")
	if s.flash != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(t.Muted).Render("  "+s.flash)
	}
	if s.confirm.Open() {
		return s.confirm.Render(width, height)
	}
	return body
}

func (s *System) renderIdentityCard(width int) string {
	t := s.ctx.Theme
	if s.info == nil {
		return Card(t, width, " Identity ", "\n  …\n", false)
	}
	props := [][2]string{
		{"Model", s.info.Model},
		{"Serial", s.info.Serial},
		{"Hostname", s.info.Hostname},
		{"DSM version", coalesce(s.info.DSMVersion, s.info.Version)},
		{"DSM build", s.info.Build},
		{"CPU", strings.TrimSpace(s.info.CPUVendor + " " + s.info.CPUFamily + " " + s.info.CPUSeries)},
		{"CPU cores", s.info.CPUCores},
		{"CPU clock", fmt.Sprintf("%d MHz", s.info.CPUClock)},
		{"RAM installed", fmt.Sprintf("%d MB", s.info.RAMTotalMB)},
		{"Time zone", strings.TrimSpace(s.info.TimeZone + " " + s.info.TimeZoneDesc)},
		{"System time", s.info.SystemTime},
		{"Uptime", HumanDurationFromDSMUptime(s.info.UptimeSeconds).String()},
		{"Temperature", fmt.Sprintf("%d °C", s.info.Temperature)},
		{"NTP", boolWord(s.info.NTPEnabled) + "  " + s.info.NTPServer},
	}
	return propsCard(t, width, " ⌂  Identity ", props)
}

func (s *System) renderRuntimeCard(width int) string {
	t := s.ctx.Theme
	if s.util == nil {
		return propsCard(t, width, " Runtime ", [][2]string{{"Status", "loading…"}})
	}
	props := [][2]string{
		{"CPU load (1m)", fmt.Sprintf("%d%%", s.util.CPU.OneMinLoad)},
		{"CPU load (5m)", fmt.Sprintf("%d%%", s.util.CPU.FiveMinLoad)},
		{"CPU load (15m)", fmt.Sprintf("%d%%", s.util.CPU.FifteenMinLoad)},
		{"Memory usage", fmt.Sprintf("%d%%", s.util.Memory.RealUsage)},
		{"Memory used", HumanBytes(uint64(s.util.Memory.TotalReal-s.util.Memory.AvailReal) * 1024)},
		{"Memory total", HumanBytes(uint64(s.util.Memory.TotalReal) * 1024)},
		{"Swap usage", fmt.Sprintf("%d%%", s.util.Memory.SwapUsage)},
		{"Buffer", HumanBytes(uint64(s.util.Memory.Buffer) * 1024)},
		{"Cached", HumanBytes(uint64(s.util.Memory.Cached) * 1024)},
	}
	return propsCard(t, width, " Runtime ", props)
}

func (s *System) renderActionsCard(width int) string {
	t := s.ctx.Theme
	chips := []string{
		t.Chip(t.Warn).Render(" B · Reboot "),
		t.Chip(t.Error).Render(" S · Shut down "),
	}
	body := t.Title().Render(" Power ") + "\n  " + strings.Join(chips, "   ") + "\n" +
		lipgloss.NewStyle().Foreground(t.Faint).Render(
			"  Both actions ask for confirmation before sending the request.")
	return t.Card(false).Width(width - 2).Render(body)
}

func boolWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

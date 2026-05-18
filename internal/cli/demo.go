package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/janbaraniewski/synology-ctl/internal/demo"
	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
	"github.com/janbaraniewski/synology-ctl/internal/tui/views"
)

// newDemoCmd returns the `synoctl demo` subcommand. It starts an
// in-process mock DSM server, points a regular dsm.Client at it, and
// launches the full TUI — every sidebar entry renders against canned
// data so the binary can be screenshotted without a real NAS.
//
// Zero config is touched: the demo doesn't read ~/.config/synoctl
// and doesn't write anything (Keychain or otherwise). It exists only
// for the duration of the TUI session.
func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Launch the TUI against an in-process mock DSM (no real NAS required)",
		Long: `Launch the synoctl TUI against an in-process mock DSM server populated
with realistic demo data. Every view renders without touching a real
device, so the binary is screenshot-ready out of the box.

Use this for README / docs screenshots, or to explore synoctl's surface
before pointing it at your own NAS.

Nothing is read from or written to your config (~/.config/synoctl) or
Keychain — the mock server lives only for the duration of this session.`,
		RunE: runDemo,
	}
}

// runDemo wires the mock server + a real dsm.Client + the full TUI.
func runDemo(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	srv := demo.New()
	defer srv.Close()

	host, port, err := splitHostPort(srv.HostPort())
	if err != nil {
		return fmt.Errorf("demo: parse mock server address: %w", err)
	}

	client, err := dsm.New(dsm.Options{
		Scheme:  "http", // local httptest server is plaintext
		Host:    host,
		Port:    port,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("demo: build dsm client: %w", err)
	}

	authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := client.Login(authCtx, dsm.LoginRequest{
		Account:    "demo",
		Password:   "demo",
		DeviceName: "synoctl-demo",
	}); err != nil {
		return fmt.Errorf("demo: login to mock server: %w", err)
	}
	defer func() {
		shutdownCtx, c2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer c2()
		_ = client.Logout(shutdownCtx)
	}()

	// Pre-populate the API path table so c.Supports() returns true for
	// every advertised endpoint — without this, the views guarding on
	// Supports() would render empty-states.
	infoCtx, c3 := context.WithTimeout(ctx, 5*time.Second)
	defer c3()
	_ = client.Info(infoCtx)

	theme := tui.DefaultTheme()
	logger := log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false, Prefix: "tui-demo"})
	vctx := tui.ViewContext{Client: client, Theme: theme, Keys: tui.DefaultKeys(), Logger: logger}

	sections := []tui.NavSection{
		{Name: "Overview", Views: []tui.View{
			views.NewDashboard(vctx),
		}},
		{Name: "Storage", Views: []tui.View{
			views.NewVolumes(vctx),
			views.NewDisks(vctx),
			views.NewShares(vctx),
			views.NewFiles(vctx),
			views.NewUsage(vctx),
			views.NewQuotas(vctx),
		}},
		{Name: "Apps", Views: []tui.View{
			views.NewApps(vctx),
			views.NewContainers(vctx),
		}},
		{Name: "Backup", Views: []tui.View{
			views.NewHyperBackup(vctx),
			views.NewActiveBackup(vctx),
		}},
		{Name: "Services", Views: []tui.View{
			views.NewDrive(vctx),
			views.NewSurveillance(vctx),
		}},
		{Name: "Security", Views: []tui.View{
			views.NewCerts(vctx),
			views.NewSecurityAdvisor(vctx),
			views.NewFirewall(vctx),
		}},
		{Name: "System", Views: []tui.View{
			views.NewAdminPage(vctx),
			views.NewSchedTasks(vctx),
			views.NewDDNS(vctx),
			views.NewNotifications(vctx),
		}},
		{Name: "Tools", Views: []tui.View{
			views.NewExplorer(vctx),
		}},
	}
	app := tui.NewApp(client, theme, logger, sections)
	prog := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = prog.Run()
	return err
}

// splitHostPort parses a "host:port" string into (host, port).
func splitHostPort(hp string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hp)
	if err != nil {
		// httptest.Server.URL gives a parseable URL; bail out via
		// net/url as a fallback for anything weirder.
		u, uerr := url.Parse("http://" + hp)
		if uerr != nil {
			return "", 0, err
		}
		host = u.Hostname()
		portStr = u.Port()
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return host, port, nil
}

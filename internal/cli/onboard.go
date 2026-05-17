package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/config"
	"github.com/janbaraniewski/synology-ctl/internal/discover"
	"github.com/janbaraniewski/synology-ctl/internal/dsm"
)

// Onboard runs the full first-run UX: scan → pick device → enter credentials
// → log in → persist profile + Keychain. It returns the saved profile so the
// caller can hand it to startTUI without an extra round-trip to disk.
//
// The flow is also used by `synoctl login` (always shows the device picker,
// even when only one device was discovered, so the user can review what they
// are connecting to).
func Onboard(ctx context.Context, cfg *config.Config) (*config.Profile, error) {
	banner()

	// 1. Scan for devices with a spinner so it never feels like a hang.
	var devices []discover.Device
	scanErr := spinner.New().
		Type(spinner.Line).
		Title("  Scanning the local network for Synology devices…").
		Action(func() {
			scanCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			devices, _ = discover.Scan(scanCtx, 7*time.Second)
		}).
		Run()
	if scanErr != nil {
		return nil, scanErr
	}

	// 2. Pick a host. We always show a list (even single-result) plus a
	//    "enter manually" escape hatch so users on routed networks aren't
	//    stuck.
	pick, err := pickDevice(devices)
	if err != nil {
		return nil, err
	}

	// 3. Credentials.
	creds, err := promptCreds(pick.suggestedUser)
	if err != nil {
		return nil, err
	}

	// 4. Login (with automatic scheme fallback).
	resp, scheme, port, err := loginWithFallback(ctx, pick, creds)
	if err != nil {
		return nil, err
	}

	// 5. Persist.
	profile := config.Profile{
		Name:     pick.name,
		Host:     pick.host,
		Port:     port,
		Scheme:   scheme,
		Username: creds.user,
		Insecure: pick.insecure,
		DeviceID: resp.DID,
	}
	cfg.Upsert(profile)
	if cfg.Default == "" {
		cfg.Default = profile.Name
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	if err := config.SavePassword(profile.Host, profile.Username, creds.password); err != nil {
		return nil, fmt.Errorf("save password to keychain: %w", err)
	}

	successBanner(profile)
	return &profile, nil
}

// onboardingPick aggregates the user's host selection plus the hints we
// derived (or guessed) for scheme/port/name.
type onboardingPick struct {
	host          string
	port          int
	scheme        string
	insecure      bool
	name          string
	suggestedUser string
}

type credentials struct {
	user, password, otp string
}

const (
	mauve      = lipgloss.Color("#cba6f7")
	muted      = lipgloss.Color("#a6adc8")
	accentBlue = lipgloss.Color("#89b4fa")
)

func banner() {
	title := lipgloss.NewStyle().Foreground(mauve).Bold(true).Render(" synoctl ")
	sub := lipgloss.NewStyle().Foreground(muted).Render("welcome — let's get connected to your NAS")
	fmt.Println()
	fmt.Println(" " + title + "  " + sub)
	fmt.Println()
}

func successBanner(p config.Profile) {
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Bold(true).Render(" ✓ ")
	fmt.Println()
	fmt.Println(ok + lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4")).Render("Saved profile ") +
		lipgloss.NewStyle().Foreground(accentBlue).Bold(true).Render(p.Name) +
		lipgloss.NewStyle().Foreground(muted).Render("  ("+p.Username+"@"+p.Host+")"))
	fmt.Println()
}

// pickDevice always presents a selection list — single-result included —
// followed by a "manual entry" sentinel so users can type in hosts that
// don't broadcast mDNS.
func pickDevice(devices []discover.Device) (*onboardingPick, error) {
	const manualValue = "__manual__"

	opts := make([]huh.Option[string], 0, len(devices)+1)
	for _, d := range devices {
		label := fmt.Sprintf("%-22s  %-18s  %s  · port %d",
			truncTo(d.Hostname, 22),
			truncTo(d.PrimaryAddr(), 18),
			truncTo(coalesce(d.Model, "Synology"), 14),
			d.Port,
		)
		val := d.PrimaryAddr() + "|" + strconv.Itoa(d.Port) + "|" + boolStr(d.Secure) + "|" + d.Hostname
		opts = append(opts, huh.NewOption(label, val))
	}
	opts = append(opts, huh.NewOption("⌨  Enter host manually", manualValue))

	var pick string
	title := "Discovered devices"
	desc := "Pick a NAS to log into"
	if len(devices) == 0 {
		title = "No devices discovered"
		desc = "Nothing answered on the local mDNS bus — enter a host manually"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description(desc).
			Options(opts...).
			Value(&pick),
	)).WithTheme(theme())
	if err := form.Run(); err != nil {
		return nil, err
	}

	if pick == manualValue {
		return manualEntry()
	}
	parts := splitN(pick, "|", 4)
	port, _ := strconv.Atoi(parts[1])
	secure := parts[2] == "1"
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return &onboardingPick{
		host:   parts[0],
		port:   port,
		scheme: scheme,
		name:   parts[3],
	}, nil
}

func manualEntry() (*onboardingPick, error) {
	var (
		host     string
		portStr  = "5001"
		scheme   = "https"
		insecure bool
	)
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Hostname or IP").Value(&host).Validate(notEmpty("host")),
		huh.NewSelect[string]().
			Title("Scheme").
			Options(huh.NewOption("https (recommended)", "https"), huh.NewOption("http", "http")).
			Value(&scheme),
		huh.NewInput().Title("Port").Value(&portStr),
		huh.NewConfirm().Title("Skip TLS verification?").Description("Required for self-signed certs.").Value(&insecure),
	)).WithTheme(theme())
	if err := form.Run(); err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %q", portStr)
	}
	return &onboardingPick{
		host:     host,
		port:     port,
		scheme:   scheme,
		insecure: insecure,
		name:     host,
	}, nil
}

func promptCreds(suggested string) (*credentials, error) {
	c := &credentials{user: suggested}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Username").
			Description("Your DSM account name").
			Value(&c.user).
			Validate(notEmpty("username")),
		huh.NewInput().
			Title("Password").
			EchoMode(huh.EchoModePassword).
			Value(&c.password).
			Validate(notEmpty("password")),
		huh.NewInput().
			Title("2FA / OTP code").
			Description("Leave blank if 2-step verification is not enabled").
			Value(&c.otp),
	)).WithTheme(theme())
	if err := form.Run(); err != nil {
		return nil, err
	}
	return c, nil
}

func loginWithFallback(ctx context.Context, pick *onboardingPick, creds *credentials) (*dsm.LoginResponse, string, int, error) {
	deadline, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	attempt := func(scheme string, port int) (*dsm.LoginResponse, error) {
		fmt.Printf("  %s %s://%s:%d as %s\n",
			lipgloss.NewStyle().Foreground(mauve).Render("→"),
			scheme, pick.host, port, creds.user)
		client, err := dsm.New(dsm.Options{
			Scheme:   scheme,
			Host:     pick.host,
			Port:     port,
			Insecure: pick.insecure,
			Timeout:  20 * time.Second,
		})
		if err != nil {
			return nil, err
		}
		var resp *dsm.LoginResponse
		spinErr := spinner.New().
			Type(spinner.Dots).
			Title("    authenticating…").
			Action(func() {
				resp, err = client.Login(deadline, dsm.LoginRequest{
					Account:    creds.user,
					Password:   creds.password,
					OTP:        creds.otp,
					DeviceName: "synoctl-" + hostnameOr("local"),
				})
				_ = client.Logout(deadline)
			}).
			Run()
		if spinErr != nil {
			return nil, spinErr
		}
		return resp, err
	}

	resp, err := attempt(pick.scheme, pick.port)
	scheme, port := pick.scheme, pick.port
	if err != nil && isProtocolMismatch(err) {
		altScheme, altPort := flipScheme(scheme, port)
		fmt.Printf("  %s protocol mismatch — retrying with %s:%d\n",
			lipgloss.NewStyle().Foreground(muted).Render("…"), altScheme, altPort)
		resp, err = attempt(altScheme, altPort)
		scheme, port = altScheme, altPort
	}
	if err != nil {
		if dsm.IsOTPRequired(err) {
			return nil, "", 0, errors.New("OTP required — run again and enter the 6-digit code from your authenticator")
		}
		return nil, "", 0, err
	}
	return resp, scheme, port, nil
}

func theme() *huh.Theme {
	return huh.ThemeCatppuccin()
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncTo(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

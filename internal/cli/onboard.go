package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

	// 1. If the host has more than one usable interface (VPNs, multiple
	//    LANs), ask the user which to scan. mDNS doesn't always cross
	//    those boundaries — picking the right one matters when the NAS
	//    is behind a VPN.
	ifaces, err := pickInterfaces()
	if err != nil {
		return nil, err
	}

	// 2. Scan for devices on the selected interfaces.
	var devices []discover.Device
	scanErr := spinner.New().
		Type(spinner.Line).
		Title("  Scanning for Synology devices…").
		Action(func() {
			scanCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			devices, _ = discover.ScanInterfaces(scanCtx, 7*time.Second, ifaces)
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
	//
	// DSM returns the device token under "did" on some firmware
	// (modern DSM 7.2) and "device_id" on others (DSM 7.0.1, the
	// reference device for this project). Without coalescing both
	// fields here, profile.DeviceID can end up empty even though the
	// server happily issued a token — and every subsequent launch
	// then asks for OTP again, triggering an unwanted re-onboard.
	did := resp.DID
	if did == "" {
		did = resp.DeviceID
	}
	profile := config.Profile{
		Name:     pick.name,
		Host:     pick.host,
		Port:     port,
		Scheme:   scheme,
		Username: creds.user,
		Insecure: pick.insecure,
		DeviceID: did,
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
	brand := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1e1e2e")).
		Background(mauve).
		Bold(true).
		Padding(0, 1).
		Render("synoctl")
	tagline := lipgloss.NewStyle().Foreground(muted).Render(
		"a TUI for your Synology NAS · let's get you connected")
	fmt.Println()
	fmt.Println("  " + brand + "  " + tagline)
	fmt.Println()
}

func successBanner(p config.Profile) {
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Bold(true).Render(" ✓ ")
	fmt.Println()
	fmt.Println(ok + lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4")).Render("Saved profile ") +
		lipgloss.NewStyle().Foreground(accentBlue).Bold(true).Render(p.Name) +
		lipgloss.NewStyle().Foreground(muted).Render("  ("+p.Username+"@"+p.Host+")"))
	if p.DeviceID != "" {
		fmt.Println(lipgloss.NewStyle().Foreground(muted).Render(
			"   device token captured — next launch will skip the OTP prompt"))
	} else {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af")).Render(
			"   ⚠ no device token returned — next launch will ask for OTP again"))
	}
	fmt.Println()
}

// pickDevice always presents a selection list — single-result included —
// followed by a "manual entry" sentinel so users can type in hosts that
// don't broadcast mDNS or aren't reachable from any scanned subnet.
func pickDevice(devices []discover.Device) (*onboardingPick, error) {
	const manualValue = "__manual__"

	// Compute column widths so labels stay aligned regardless of the
	// hostname / network length variation.
	hostW, addrW, netW := 14, 15, 12
	for _, d := range devices {
		if w := len(d.Hostname); w > hostW {
			hostW = w
		}
		if w := len(d.PrimaryAddr()); w > addrW {
			addrW = w
		}
		if w := len(d.Network); w > netW {
			netW = w
		}
	}
	if hostW > 28 {
		hostW = 28
	}
	if addrW > 22 {
		addrW = 22
	}
	if netW > 24 {
		netW = 24
	}

	opts := make([]huh.Option[string], 0, len(devices)+1)
	for _, d := range devices {
		scheme := "http"
		if d.Secure {
			scheme = "https"
		}
		label := fmt.Sprintf("%-*s  %-*s  %-*s  %s  %s:%d",
			hostW, truncTo(d.Hostname, hostW),
			addrW, truncTo(d.PrimaryAddr(), addrW),
			netW, truncTo(d.Network, netW),
			truncTo(coalesce(d.Model, "Synology"), 12),
			scheme, d.Port,
		)
		val := d.PrimaryAddr() + "|" + strconv.Itoa(d.Port) + "|" + boolStr(d.Secure) + "|" + d.Hostname
		opts = append(opts, huh.NewOption(label, val))
	}
	opts = append(opts, huh.NewOption("Enter host manually…", manualValue))

	var pick string
	title := fmt.Sprintf("Pick a NAS  (%d discovered)", len(devices))
	if len(devices) == 0 {
		title = "No devices discovered — enter one manually"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
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
				// Intentionally no Logout here. The verification login
				// is the same session the user will continue with, and
				// some DSM firmwares revoke the device token when the
				// SID is closed too aggressively — leaving the user
				// re-onboarding every launch. The server-side session
				// expires on its own after a few minutes of inactivity.
			}).
			Run()
		if spinErr != nil {
			return nil, spinErr
		}
		return resp, err
	}

	resp, err := attempt(pick.scheme, pick.port)
	scheme, port := pick.scheme, pick.port

	// DSM ships a self-signed cert by default. When the user picked an
	// https endpoint (mDNS-discovered or tailnet) we hit a verification
	// error — ask them once, then retry with verification skipped.
	if err != nil && isSelfSignedCert(err) {
		ok, askErr := confirmSelfSignedCert(pick.host)
		if askErr != nil {
			return nil, "", 0, askErr
		}
		if !ok {
			return nil, "", 0, errors.New("aborted — TLS cert not trusted; rerun and pick http or accept the self-signed cert")
		}
		pick.insecure = true
		resp, err = attempt(scheme, port)
	}

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

// isSelfSignedCert detects DSM's "synology" self-signed certificate
// rejection from Go's TLS stack.
func isSelfSignedCert(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "x509:") &&
		(strings.Contains(s, "certificate is not trusted") ||
			strings.Contains(s, "certificate signed by unknown authority") ||
			strings.Contains(s, "unable to verify") ||
			strings.Contains(s, "self-signed"))
}

// confirmSelfSignedCert is a small huh prompt that asks the user to
// trust the self-signed cert. We say yes/no, with no defaulting in
// either direction.
func confirmSelfSignedCert(host string) (bool, error) {
	var trust bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("TLS certificate not trusted").
			Description(host + " is using a self-signed certificate. This is normal for DSM. Trust it for this profile?").
			Affirmative("Trust").
			Negative("Cancel").
			Value(&trust),
	)).WithTheme(theme())
	if err := form.Run(); err != nil {
		return false, err
	}
	return trust, nil
}

// theme is the synoctl-branded huh theme. We start from huh's
// Catppuccin base and tighten a few slots so it matches the TUI
// palette: mauve accent, soft border, generous padding on the focused
// option.
func theme() *huh.Theme {
	t := huh.ThemeCatppuccin()
	mauve := lipgloss.Color("#cba6f7")
	text := lipgloss.Color("#cdd6f4")
	muted := lipgloss.Color("#a6adc8")
	subtle := lipgloss.Color("#6c7086")
	bg := lipgloss.Color("#1e1e2e")

	t.Focused.Title = t.Focused.Title.Foreground(mauve).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(mauve)
	t.Focused.Description = t.Focused.Description.Foreground(muted)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(mauve).Bold(true).SetString("▸ ")
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(mauve).Bold(true).SetString("▸ ")
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(mauve).Bold(true)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(text)
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(subtle).SetString("  ")
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(mauve).SetString("✓ ")
	t.Focused.Base = t.Focused.Base.BorderForeground(mauve).BorderLeft(true).PaddingLeft(1)

	t.Help.Ellipsis = t.Help.Ellipsis.Foreground(subtle)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(mauve)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(muted)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(subtle)
	t.Help.FullKey = t.Help.FullKey.Foreground(mauve)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(muted)
	t.Help.FullSeparator = t.Help.FullSeparator.Foreground(subtle)
	_ = bg
	return t
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

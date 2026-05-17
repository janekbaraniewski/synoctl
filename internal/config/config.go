// Package config persists synoctl preferences to ~/.config/synoctl/config.yaml.
// Secrets live in the macOS Keychain (see keychain.go).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Profile is a single NAS connection profile.
type Profile struct {
	Name     string `yaml:"name"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Scheme   string `yaml:"scheme"` // http | https
	Username string `yaml:"username"`
	Insecure bool   `yaml:"insecure,omitempty"`  // skip TLS verification
	DeviceID string `yaml:"device_id,omitempty"` // stored to skip OTP next time
}

// Config is the on-disk shape. Default points to one of Profiles by Name.
type Config struct {
	Default  string    `yaml:"default,omitempty"`
	Profiles []Profile `yaml:"profiles"`
	UI       UIConfig  `yaml:"ui"`
}

// UIConfig captures interface preferences.
type UIConfig struct {
	Theme        string `yaml:"theme,omitempty"`         // catppuccin-mocha | catppuccin-latte | auto
	Refresh      string `yaml:"refresh,omitempty"`       // dashboard refresh, e.g. "2s"
	UnicodeIcons bool   `yaml:"unicode_icons,omitempty"` // false → ASCII fallback
}

// Path returns the absolute config file path.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "synoctl", "config.yaml"), nil
}

// Load reads the config from disk. A missing file returns an empty Config
// with sensible defaults, not an error — first-run is normal.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return defaultConfig(), nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "auto"
	}
	if c.UI.Refresh == "" {
		c.UI.Refresh = "2s"
	}
	if !c.UI.UnicodeIcons {
		c.UI.UnicodeIcons = true
	}
	return &c, nil
}

// Save writes the config to disk, creating parent dirs as needed.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Active returns the default profile or the first one if none is marked.
func (c *Config) Active() (*Profile, bool) {
	if len(c.Profiles) == 0 {
		return nil, false
	}
	if c.Default != "" {
		for i := range c.Profiles {
			if c.Profiles[i].Name == c.Default {
				return &c.Profiles[i], true
			}
		}
	}
	return &c.Profiles[0], true
}

// Upsert inserts or replaces a profile by name.
func (c *Config) Upsert(p Profile) {
	for i := range c.Profiles {
		if c.Profiles[i].Name == p.Name {
			c.Profiles[i] = p
			return
		}
	}
	c.Profiles = append(c.Profiles, p)
}

// Remove drops a profile by name; returns true when something was removed.
func (c *Config) Remove(name string) bool {
	for i := range c.Profiles {
		if c.Profiles[i].Name == name {
			c.Profiles = append(c.Profiles[:i], c.Profiles[i+1:]...)
			if c.Default == name {
				c.Default = ""
			}
			return true
		}
	}
	return false
}

func defaultConfig() *Config {
	return &Config{
		UI: UIConfig{
			Theme:        "auto",
			Refresh:      "2s",
			UnicodeIcons: true,
		},
	}
}

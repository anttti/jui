// Package config loads the jira-tui TOML configuration file and resolves
// runtime paths.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the effective runtime configuration.
type Config struct {
	Site            string
	Email           string
	Token           string
	JQL             string
	SyncInterval    time.Duration
	InitialLookback time.Duration
	Theme           string

	// BaseURL is "https://" + Site by default. Exposed as a field so tests
	// can point at a local httptest server.
	BaseURL string

	// Resolved paths.
	ConfigDir string
	DBPath    string
	LogDir    string
}

// rawConfig is the TOML wire format.
type rawConfig struct {
	Site             string `toml:"site"`
	Email            string `toml:"email"`
	APIToken         string `toml:"api_token"`
	APITokenKeychain string `toml:"api_token_keychain"`
	JQL              string `toml:"jql"`
	SyncInterval     string `toml:"sync_interval"`
	InitialLookback  string `toml:"initial_lookback"`

	UI struct {
		Theme string `toml:"theme"`
	} `toml:"ui"`
}

// DefaultPath returns the expected config path under $HOME on macOS.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "jira-tui", "config.toml")
}

// Load reads the config from DefaultPath.
func Load() (*Config, error) { return LoadFrom(DefaultPath()) }

// LoadFrom reads a config from the given path.
func LoadFrom(path string) (*Config, error) {
	var raw rawConfig
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return build(&raw, path)
}

func build(raw *rawConfig, path string) (*Config, error) {
	if raw.Site == "" {
		return nil, errors.New("config: site is required")
	}
	if raw.Email == "" {
		return nil, errors.New("config: email is required")
	}

	interval := 5 * time.Minute
	if raw.SyncInterval != "" {
		d, err := ParseDuration(raw.SyncInterval)
		if err != nil {
			return nil, fmt.Errorf("config: sync_interval: %w", err)
		}
		interval = d
	}

	lookback := 90 * 24 * time.Hour
	if raw.InitialLookback != "" {
		d, err := ParseDuration(raw.InitialLookback)
		if err != nil {
			return nil, fmt.Errorf("config: initial_lookback: %w", err)
		}
		lookback = d
	}

	// Resolve token: env > config > (keychain — not yet implemented).
	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		token = raw.APIToken
	}

	cfgDir := filepath.Dir(path)
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, "Library", "Logs", "jira-tui")

	return &Config{
		Site:            raw.Site,
		Email:           raw.Email,
		Token:           token,
		JQL:             raw.JQL,
		SyncInterval:    interval,
		InitialLookback: lookback,
		Theme:           raw.UI.Theme,
		BaseURL:         "https://" + raw.Site,
		ConfigDir:       cfgDir,
		DBPath:          filepath.Join(cfgDir, "jira.db"),
		LogDir:          logDir,
	}, nil
}

// ParseDuration accepts time.ParseDuration plus a "d" (day) suffix.
// Examples: "5m", "90d", "24h".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}


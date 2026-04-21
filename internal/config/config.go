// Package config loads the jui TOML configuration file and resolves
// runtime paths.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// defaultConfigBody is written on first run when no config file exists.
const defaultConfigBody = `# jui configuration

site = ""                      # e.g. "acme.atlassian.net"
email = ""                     # your Atlassian account email
# Create a token at https://id.atlassian.com/manage-profile/security/api-tokens
# You can also set the JIRA_API_TOKEN environment variable instead.
api_token = ""
jql = "assignee = currentUser() AND resolution = Unresolved"
sync_interval = "5m"
initial_lookback = "90d"

[ui]
theme = "dark"
`

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

// DefaultPath returns the expected config path under $HOME.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "jui", "jui.toml")
}

// EnsureDefault creates the parent directory and writes a default config file
// at path if one does not already exist. A nil error with created=false means
// the file was already there.
func EnsureDefault(path string) (created bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf("stat %s: %w", path, statErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(defaultConfigBody), 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// Load reads the config from DefaultPath, creating a default file on first run.
func Load() (*Config, error) {
	path := DefaultPath()
	if _, err := EnsureDefault(path); err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads a config from the given path.
func LoadFrom(path string) (*Config, error) {
	var raw rawConfig
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return build(&raw, path)
}

func build(raw *rawConfig, path string) (*Config, error) {
	// Resolve token: env > config.
	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		token = raw.APIToken
	}

	var missing []string
	if raw.Site == "" {
		missing = append(missing, "site")
	}
	if raw.Email == "" {
		missing = append(missing, "email")
	}
	if token == "" {
		missing = append(missing, "api_token")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing required field(s): %s — edit %s (or set JIRA_API_TOKEN for the token)", strings.Join(missing, ", "), path)
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


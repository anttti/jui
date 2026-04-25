package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anttti/j/internal/config"
)

func TestEnsureDefault_WritesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "jui.toml")
	created, err := config.EnsureDefault(path)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first call")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "site = ") {
		t.Fatalf("default body missing site key:\n%s", string(body))
	}
}

func TestEnsureDefault_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jui.toml")
	if err := os.WriteFile(path, []byte("# user content"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := config.EnsureDefault(path)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if created {
		t.Fatalf("expected created=false when file exists")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# user content" {
		t.Fatalf("file overwritten unexpectedly:\n%s", string(body))
	}
}

func TestDefaultPath_PointsUnderHome(t *testing.T) {
	got := config.DefaultPath()
	home, _ := os.UserHomeDir()
	if home != "" && !strings.HasPrefix(got, home) {
		t.Fatalf("DefaultPath()=%q should start with %q", got, home)
	}
	if !strings.HasSuffix(got, "jui.toml") {
		t.Fatalf("DefaultPath()=%q should end in jui.toml", got)
	}
}

func TestParseDuration_RejectsBadDayValue(t *testing.T) {
	if _, err := config.ParseDuration("xd"); err == nil {
		t.Fatalf("expected error for 'xd'")
	}
}

func TestParseDuration_ZeroDays(t *testing.T) {
	d, err := config.ParseDuration("0d")
	if err != nil {
		t.Fatalf("ParseDuration: %v", err)
	}
	if d != 0 {
		t.Fatalf("0d -> %v", d)
	}
}

func TestParseDuration_HourMinuteVariants(t *testing.T) {
	for input, want := range map[string]time.Duration{
		"1h":    time.Hour,
		"30m":   30 * time.Minute,
		"1h30m": 90 * time.Minute,
	} {
		got, err := config.ParseDuration(input)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseDuration(%q)=%v want %v", input, got, want)
		}
	}
}

func TestLoadFrom_RejectsBadDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
site = "x.atlassian.net"
email = "e@x"
api_token = "tok"
sync_interval = "5banana"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadFrom(path)
	if err == nil {
		t.Fatalf("expected error on bad sync_interval")
	}
	if !strings.Contains(err.Error(), "sync_interval") {
		t.Fatalf("error should mention sync_interval; got %v", err)
	}
}

func TestLoadFrom_RejectsBadInitialLookback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
site = "x.atlassian.net"
email = "e@x"
api_token = "tok"
initial_lookback = "12bogus"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadFrom(path)
	if err == nil {
		t.Fatalf("expected error on bad initial_lookback")
	}
	if !strings.Contains(err.Error(), "initial_lookback") {
		t.Fatalf("error should mention initial_lookback; got %v", err)
	}
}

func TestLoadFrom_MissingFileError(t *testing.T) {
	_, err := config.LoadFrom(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatalf("expected error reading missing file")
	}
}

func TestLoad_UsesDefaultPathAndCreatesFile(t *testing.T) {
	// Setting HOME to a tempdir reroutes config.DefaultPath() under it so
	// Load() exercises EnsureDefault + LoadFrom end-to-end without
	// touching the real user config.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// First call: file is created. Default body is missing required fields,
	// so Load returns an error — but a meaningful one ("missing required").
	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected missing-fields error from default body")
	}
	if !strings.Contains(err.Error(), "missing required") {
		t.Fatalf("error should mention missing fields; got %v", err)
	}

	// Verify the default file landed where DefaultPath() points.
	if _, statErr := os.Stat(config.DefaultPath()); statErr != nil {
		t.Fatalf("DefaultPath file not created: %v", statErr)
	}
}

func TestLoad_DerivesPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jui.toml")
	body := `
site = "x.atlassian.net"
email = "e@x"
api_token = "tok"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.ConfigDir != dir {
		t.Fatalf("ConfigDir=%q want %q", cfg.ConfigDir, dir)
	}
	if cfg.DBPath != filepath.Join(dir, "jira.db") {
		t.Fatalf("DBPath=%q", cfg.DBPath)
	}
	if cfg.BaseURL != "https://x.atlassian.net" {
		t.Fatalf("BaseURL=%q", cfg.BaseURL)
	}
}

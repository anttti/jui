package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anttti/j/internal/config"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_ParsesFieldsAndResolvesToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `
site  = "acme.atlassian.net"
email = "me@acme.com"
api_token = "cfg-token"
jql = "assignee = currentUser()"
sync_interval = "5m"
initial_lookback = "30d"

[ui]
theme = "dark"
`)
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Site != "acme.atlassian.net" || cfg.Email != "me@acme.com" {
		t.Fatalf("site/email: %+v", cfg)
	}
	if cfg.Token != "cfg-token" {
		t.Fatalf("token=%q", cfg.Token)
	}
	if cfg.JQL == "" {
		t.Fatalf("jql empty")
	}
	if cfg.SyncInterval != 5*time.Minute {
		t.Fatalf("sync interval=%v", cfg.SyncInterval)
	}
	if cfg.InitialLookback != 30*24*time.Hour {
		t.Fatalf("initial_lookback=%v", cfg.InitialLookback)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme=%q", cfg.Theme)
	}
}

func TestLoad_EnvTokenTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `
site  = "x.atlassian.net"
email = "e"
api_token = "from-config"
jql = "j"
`)
	t.Setenv("JIRA_API_TOKEN", "from-env")
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Token != "from-env" {
		t.Fatalf("token=%q want from-env", cfg.Token)
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `
site  = "x.atlassian.net"
email = "e"
api_token = "t"
jql = "j"
`)
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SyncInterval <= 0 {
		t.Fatalf("default sync_interval should be positive, got %v", cfg.SyncInterval)
	}
	if cfg.InitialLookback <= 0 {
		t.Fatalf("default lookback should be positive, got %v", cfg.InitialLookback)
	}
}

func TestParseDuration_SupportsDays(t *testing.T) {
	d, err := config.ParseDuration("7d")
	if err != nil {
		t.Fatal(err)
	}
	if d != 7*24*time.Hour {
		t.Fatalf("got %v", d)
	}
	d, err = config.ParseDuration("15m")
	if err != nil {
		t.Fatal(err)
	}
	if d != 15*time.Minute {
		t.Fatalf("got %v", d)
	}
}

func TestLoad_MissingFieldsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `jql = "x"`) // missing site, email
	_, err := config.LoadFrom(path)
	if err == nil {
		t.Fatalf("expected error on missing fields")
	}
}

// Package cmd contains the subcommand entrypoints. Each runner is a plain
// function wired together from the internal packages so tests can assemble
// real dependencies without going through cobra.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/config"
	"github.com/anttti/j/internal/jira"
	"github.com/anttti/j/internal/launchd"
	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/platform"
	"github.com/anttti/j/internal/store/sqlitestore"
	syncpkg "github.com/anttti/j/internal/sync"
	"github.com/anttti/j/internal/tui"
)

// OpenStore opens the SQLite store at cfg.DBPath.
func OpenStore(cfg *config.Config) (*sqlitestore.Store, error) {
	return sqlitestore.Open(cfg.DBPath)
}

// NewJiraClient constructs a Jira client from config.
func NewJiraClient(cfg *config.Config) (*jira.Client, error) {
	return jira.New(jira.Config{
		BaseURL: cfg.BaseURL,
		Email:   cfg.Email,
		Token:   cfg.Token,
	})
}

// Sync runs a single sync cycle.
func Sync(ctx context.Context, cfg *config.Config, stdout io.Writer) error {
	st, err := OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()
	client, err := NewJiraClient(cfg)
	if err != nil {
		return err
	}
	eng := syncpkg.New(client, st, syncpkg.WithInitialLookback(cfg.InitialLookback))
	start := time.Now()
	if err := eng.Run(ctx, cfg.JQL); err != nil {
		return err
	}
	issues, total, _ := st.List(ctx, model.Filter{}, model.Page{Limit: 1})
	_ = issues
	fmt.Fprintf(stdout, "sync: ok (%d issues total) in %s\n", total, time.Since(start).Round(time.Millisecond))
	return nil
}

// Daemon runs sync cycles forever, respecting ctx cancellation.
func Daemon(ctx context.Context, cfg *config.Config, stdout io.Writer) error {
	for {
		err := Sync(ctx, cfg, stdout)
		if err != nil {
			fmt.Fprintf(stdout, "sync error: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.SyncInterval):
		}
	}
}

// TUI launches the interactive terminal UI.
func TUI(ctx context.Context, cfg *config.Config) error {
	st, err := OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()
	client, err := NewJiraClient(cfg)
	if err != nil {
		return err
	}
	engine := syncpkg.New(client, st, syncpkg.WithInitialLookback(cfg.InitialLookback))
	root := tui.New(tui.Deps{
		Store:     st,
		Fetcher:   engine,
		Opener:    platform.Opener{},
		Clipboard: platform.Clipboard{},
	})
	p := tea.NewProgram(teaAdapter{root}, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err = p.Run()
	return err
}

// teaAdapter bridges the root tui.Model (which returns its concrete type
// from Update so tests stay ergonomic) to the tea.Model interface that
// tea.Program requires.
type teaAdapter struct{ m tui.Model }

func (a teaAdapter) Init() tea.Cmd { return a.m.Init() }
func (a teaAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.Update(msg)
	return teaAdapter{next}, cmd
}
func (a teaAdapter) View() string { return a.m.View() }

// Doctor validates the config, opens the DB, and calls /myself. It prints
// a human-friendly diagnostic report to stdout. Returns an error on any
// hard failure (e.g. unreachable Jira).
func Doctor(ctx context.Context, cfg *config.Config, stdout io.Writer) error {
	fmt.Fprintf(stdout, "site:    %s\n", cfg.Site)
	fmt.Fprintf(stdout, "email:   %s\n", cfg.Email)
	fmt.Fprintf(stdout, "db:      %s\n", cfg.DBPath)
	fmt.Fprintf(stdout, "jql:     %s\n", cfg.JQL)
	if cfg.Token == "" {
		return fmt.Errorf("no API token: set JIRA_API_TOKEN or api_token in config")
	}

	st, err := OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()
	fmt.Fprintln(stdout, "store:   ok")

	client, err := NewJiraClient(cfg)
	if err != nil {
		return err
	}
	u, err := client.Myself(ctx)
	if err != nil {
		return fmt.Errorf("jira /myself: %w", err)
	}
	fmt.Fprintf(stdout, "jira:    ok (authenticated as %s, %s)\n", u.DisplayName, u.Email)
	return nil
}

// AgentInstall writes the launchd plist and bootstraps the daemon.
func AgentInstall(cfg *config.Config, stdout io.Writer) error {
	a, err := newAgent(cfg)
	if err != nil {
		return err
	}
	if err := a.Install(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed: %s\n", agentPlistPath())
	return nil
}

// AgentUninstall removes the plist and boots the daemon out of launchd.
func AgentUninstall(cfg *config.Config, stdout io.Writer) error {
	a, err := newAgent(cfg)
	if err != nil {
		return err
	}
	if err := a.Uninstall(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "uninstalled: %s\n", agentPlistPath())
	return nil
}

func newAgent(cfg *config.Config) (*launchd.Agent, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve binary path: %w", err)
	}
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return launchd.NewAgent(launchd.AgentConfig{
		PlistPath:  agentPlistPath(),
		BinaryPath: bin,
		LogDir:     cfg.LogDir,
		Domain:     "gui/" + u.Uid,
	}), nil
}

func agentPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchd.DefaultLabel+".plist")
}

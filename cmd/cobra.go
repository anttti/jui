package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/anttti/j/internal/config"
)

// NewRootCmd builds the cobra command tree.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:           "jira",
		Short:         "Terminal UI for Jira Cloud",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return TUI(signalCtx(), cfg)
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config.toml (default: ~/Library/Application Support/jira-tui/config.toml)")
	root.SetOut(stdout)
	root.SetErr(stderr)

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Run one sync cycle and exit",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return Sync(signalCtx(), cfg, stdout)
		},
	}

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the sync daemon (invoked by launchd)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			err = Daemon(signalCtx(), cfg, stdout)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, DB, and Jira reachability",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return Doctor(signalCtx(), cfg, stdout)
		},
	}

	agentCmd := &cobra.Command{Use: "agent", Short: "Manage the launchd LaunchAgent"}
	agentCmd.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install the launchd LaunchAgent",
			RunE: func(c *cobra.Command, _ []string) error {
				cfg, err := loadConfig(configPath)
				if err != nil {
					return err
				}
				return AgentInstall(cfg, stdout)
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Remove the launchd LaunchAgent",
			RunE: func(c *cobra.Command, _ []string) error {
				cfg, err := loadConfig(configPath)
				if err != nil {
					return err
				}
				return AgentUninstall(cfg, stdout)
			},
		},
	)

	root.AddCommand(syncCmd, daemonCmd, doctorCmd, agentCmd)
	return root
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// signalCtx returns a context cancelled on SIGINT/SIGTERM.
func signalCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()
	return ctx
}

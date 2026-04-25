package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/anttti/j/cmd"
)

// TestNewRootCmd_StructureAndHelp covers NewRootCmd's tree wiring: the root
// command must register the expected subcommands so that `jira <cmd> --help`
// works without crashing. Calling --help short-circuits before any
// dependency is touched (no config file read, no Jira reachability).
func TestNewRootCmd_StructureAndHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCmd(&stdout, &stderr)
	if root == nil {
		t.Fatalf("NewRootCmd returned nil")
	}
	if root.Use != "jira" {
		t.Fatalf("Use=%q want jira", root.Use)
	}

	wantSubs := []string{"sync", "daemon", "doctor", "agent"}
	gotSubs := map[string]bool{}
	for _, c := range root.Commands() {
		gotSubs[c.Name()] = true
	}
	for _, name := range wantSubs {
		if !gotSubs[name] {
			t.Errorf("missing subcommand %q (have %v)", name, gotSubs)
		}
	}
}

func TestNewRootCmd_ConfigFlagRegistered(t *testing.T) {
	root := cmd.NewRootCmd(new(bytes.Buffer), new(bytes.Buffer))
	flag := root.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatalf("--config flag missing")
	}
	if flag.DefValue != "" {
		t.Fatalf("--config default=%q want empty", flag.DefValue)
	}
}

func TestRoot_Help_PrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Terminal UI for Jira Cloud") {
		t.Fatalf("usage output missing description:\n%s", out)
	}
	for _, sub := range []string{"sync", "daemon", "doctor", "agent"} {
		if !strings.Contains(out, sub) {
			t.Errorf("usage missing %q in:\n%s", sub, out)
		}
	}
}

func TestRoot_AgentSubcommands_HaveInstallAndUninstall(t *testing.T) {
	root := cmd.NewRootCmd(new(bytes.Buffer), new(bytes.Buffer))
	var agentCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "agent" {
			agentCmd = c
			break
		}
	}
	if agentCmd == nil {
		t.Fatalf("agent subcommand not registered")
	}
	got := map[string]bool{}
	for _, c := range agentCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range []string{"install", "uninstall"} {
		if !got[name] {
			t.Errorf("agent.%s missing", name)
		}
	}
}

func TestRoot_BadConfigPath_ReturnsConfigError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCmd(&stdout, &stderr)
	// Pointing at a non-existent file under --config skips EnsureDefault
	// and goes straight to LoadFrom, which fails.
	root.SetArgs([]string{"sync", "--config", "/tmp/jui-test-does-not-exist.toml"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected config error")
	}
	if !strings.Contains(err.Error(), "config:") {
		t.Fatalf("error should be wrapped in 'config:'; got %v", err)
	}
}

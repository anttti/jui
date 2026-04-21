// Package launchd renders launchd plist files and wraps `launchctl`
// bootstrap/bootout so the daemon can be installed or removed via the
// `jira agent` subcommand.
package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// Params drives plist rendering.
type Params struct {
	Label      string
	BinaryPath string
	LogDir     string
}

// DefaultLabel is the launchd service label used by the agent.
const DefaultLabel = "com.jira-tui.daemon"

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>30</integer>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/daemon.err</string>
</dict>
</plist>
`

// RenderPlist returns the XML for the launchd plist.
func RenderPlist(p Params) (string, error) {
	if p.Label == "" {
		p.Label = DefaultLabel
	}
	t, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, p); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Ctl abstracts launchctl invocations.
type Ctl interface {
	Bootstrap(domain, plistPath string) error
	Bootout(domain, plistPath string) error
}

// Agent writes the plist and drives launchctl.
type Agent struct {
	cfg AgentConfig
}

// AgentConfig is Agent's constructor bundle.
type AgentConfig struct {
	PlistPath  string // absolute path for ~/Library/LaunchAgents/com.jira-tui.daemon.plist
	BinaryPath string
	LogDir     string
	Label      string
	Domain     string // e.g. "gui/501"
	Ctl        Ctl
}

// NewAgent constructs an Agent.
func NewAgent(cfg AgentConfig) *Agent {
	if cfg.Label == "" {
		cfg.Label = DefaultLabel
	}
	if cfg.Ctl == nil {
		cfg.Ctl = ExecCtl{}
	}
	return &Agent{cfg: cfg}
}

// Install renders the plist, creates the log dir, writes the file, and
// bootstraps it into launchd.
func (a *Agent) Install() error {
	if err := os.MkdirAll(a.cfg.LogDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.PlistPath), 0o755); err != nil {
		return fmt.Errorf("mkdir launchagents: %w", err)
	}
	body, err := RenderPlist(Params{
		Label:      a.cfg.Label,
		BinaryPath: a.cfg.BinaryPath,
		LogDir:     a.cfg.LogDir,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.cfg.PlistPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := a.cfg.Ctl.Bootstrap(a.cfg.Domain, a.cfg.PlistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	return nil
}

// Uninstall removes the plist and calls `launchctl bootout`. Missing
// plist is a no-op.
func (a *Agent) Uninstall() error {
	if _, err := os.Stat(a.cfg.PlistPath); os.IsNotExist(err) {
		return nil
	}
	if err := a.cfg.Ctl.Bootout(a.cfg.Domain, a.cfg.PlistPath); err != nil {
		// Proceed to remove the file anyway — the service may already be
		// gone.
	}
	if err := os.Remove(a.cfg.PlistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

// ExecCtl shells out to the real `launchctl`.
type ExecCtl struct{}

// Bootstrap runs `launchctl bootstrap <domain> <plist>`.
func (ExecCtl) Bootstrap(domain, plist string) error {
	return exec.Command("launchctl", "bootstrap", domain, plist).Run()
}

// Bootout runs `launchctl bootout <domain> <plist>`.
func (ExecCtl) Bootout(domain, plist string) error {
	return exec.Command("launchctl", "bootout", domain, plist).Run()
}

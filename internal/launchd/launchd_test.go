package launchd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anttti/j/internal/launchd"
)

// fakeCtl records launchctl invocations.
type fakeCtl struct {
	bootstrapCalls [][2]string
	bootoutCalls   [][2]string
	bootstrapErr   error
	bootoutErr     error
}

func (f *fakeCtl) Bootstrap(domain, plist string) error {
	f.bootstrapCalls = append(f.bootstrapCalls, [2]string{domain, plist})
	return f.bootstrapErr
}
func (f *fakeCtl) Bootout(domain, plist string) error {
	f.bootoutCalls = append(f.bootoutCalls, [2]string{domain, plist})
	return f.bootoutErr
}

func TestRenderPlist_ContainsExpectedElements(t *testing.T) {
	out, err := launchd.RenderPlist(launchd.Params{
		Label:      "com.jira-tui.daemon",
		BinaryPath: "/usr/local/bin/jira",
		LogDir:     "/tmp/logs",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "<?xml")
	mustContain(t, out, "<string>com.jira-tui.daemon</string>")
	mustContain(t, out, "<string>/usr/local/bin/jira</string>")
	mustContain(t, out, "<string>daemon</string>")
	mustContain(t, out, "<string>/tmp/logs/daemon.log</string>")
	mustContain(t, out, "<string>/tmp/logs/daemon.err</string>")
	mustContain(t, out, "<key>RunAtLoad</key>")
	mustContain(t, out, "<key>KeepAlive</key>")
}

func TestAgent_InstallWritesPlistAndCallsBootstrap(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.jira-tui.daemon.plist")
	ctl := &fakeCtl{}
	a := launchd.NewAgent(launchd.AgentConfig{
		PlistPath:  plistPath,
		BinaryPath: "/usr/local/bin/jira",
		LogDir:     "/var/logs",
		Domain:     "gui/501",
		Ctl:        ctl,
	})
	if err := a.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	b, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/usr/local/bin/jira") {
		t.Fatalf("plist missing binary:\n%s", string(b))
	}
	if len(ctl.bootstrapCalls) != 1 {
		t.Fatalf("bootstrap calls=%d", len(ctl.bootstrapCalls))
	}
	if ctl.bootstrapCalls[0] != [2]string{"gui/501", plistPath} {
		t.Fatalf("bootstrap args=%v", ctl.bootstrapCalls[0])
	}
}

func TestAgent_InstallCreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs", "jira-tui")
	ctl := &fakeCtl{}
	a := launchd.NewAgent(launchd.AgentConfig{
		PlistPath:  filepath.Join(dir, "x.plist"),
		BinaryPath: "/usr/local/bin/jira",
		LogDir:     logDir,
		Domain:     "gui/501",
		Ctl:        ctl,
	})
	if err := a.Install(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(logDir); err != nil || !info.IsDir() {
		t.Fatalf("log dir not created: %v", err)
	}
}

func TestAgent_UninstallBootsOutAndRemovesFile(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "x.plist")
	if err := os.WriteFile(plistPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctl := &fakeCtl{}
	a := launchd.NewAgent(launchd.AgentConfig{
		PlistPath:  plistPath,
		BinaryPath: "/x",
		LogDir:     dir,
		Domain:     "gui/501",
		Ctl:        ctl,
	})
	if err := a.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if len(ctl.bootoutCalls) != 1 {
		t.Fatalf("bootout calls=%d", len(ctl.bootoutCalls))
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("plist not removed: %v", err)
	}
}

func TestAgent_UninstallNoPlistIsNoop(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "missing.plist")
	ctl := &fakeCtl{}
	a := launchd.NewAgent(launchd.AgentConfig{
		PlistPath: plistPath,
		Domain:    "gui/501",
		Ctl:       ctl,
	})
	if err := a.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q in:\n%s", needle, haystack)
	}
}

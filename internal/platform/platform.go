// Package platform wraps macOS commands (open, pbcopy) as tiny, injectable
// interfaces used by the TUI.
package platform

import (
	"io"
	"os/exec"
	"strings"
)

// commandRunner abstracts the exec layer so tests can swap it for a fake.
// findFn checks if cmd is available; runFn executes it with stdin (which may
// be nil) and returns the run error.
type commandRunner struct {
	findFn func(name string) error
	runFn  func(name string, args []string, stdin io.Reader) error
}

// defaultRunner uses os/exec (the production path).
var defaultRunner = commandRunner{
	findFn: func(name string) error { _, err := exec.LookPath(name); return err },
	runFn: func(name string, args []string, stdin io.Reader) error {
		c := exec.Command(name, args...)
		if stdin != nil {
			c.Stdin = stdin
		}
		// Browser launches don't need to wait; clipboard does.
		// We rely on the caller selecting the right behaviour by passing nil
		// stdin for fire-and-forget (Open).
		if stdin == nil {
			return c.Start()
		}
		return c.Run()
	},
}

// Opener launches URLs in the user's browser via `open` / `xdg-open`.
type Opener struct{ runner commandRunner }

// Open invokes `open <url>` (macOS). Falls back to xdg-open on Linux so
// the binary is still usable for development.
func (o Opener) Open(url string) error {
	r := o.runner
	if r.findFn == nil {
		r = defaultRunner
	}
	for _, cmd := range []string{"open", "xdg-open"} {
		if err := r.findFn(cmd); err == nil {
			return r.runFn(cmd, []string{url}, nil)
		}
	}
	return nil
}

// Clipboard copies text via `pbcopy` on macOS (falls back to xclip/wl-copy
// when available).
type Clipboard struct{ runner commandRunner }

// Copy writes s to the OS clipboard.
func (c Clipboard) Copy(s string) error {
	r := c.runner
	if r.findFn == nil {
		r = defaultRunner
	}
	candidates := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
	}
	for _, cc := range candidates {
		if err := r.findFn(cc[0]); err == nil {
			return r.runFn(cc[0], cc[1:], strings.NewReader(s))
		}
	}
	return nil
}

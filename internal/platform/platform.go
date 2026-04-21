// Package platform wraps macOS commands (open, pbcopy) as tiny, injectable
// interfaces used by the TUI.
package platform

import (
	"os/exec"
	"strings"
)

// Opener launches URLs in the user's browser via `open`.
type Opener struct{}

// Open invokes `open <url>` (macOS). Falls back to xdg-open on Linux so
// the binary is still usable for development.
func (Opener) Open(url string) error {
	for _, cmd := range []string{"open", "xdg-open"} {
		if _, err := exec.LookPath(cmd); err == nil {
			return exec.Command(cmd, url).Start()
		}
	}
	return nil
}

// Clipboard copies text via `pbcopy` on macOS (falls back to xclip/wl-copy
// when available).
type Clipboard struct{}

// Copy writes s to the OS clipboard.
func (Clipboard) Copy(s string) error {
	candidates := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
	}
	for _, cc := range candidates {
		if _, err := exec.LookPath(cc[0]); err == nil {
			c := exec.Command(cc[0], cc[1:]...)
			c.Stdin = strings.NewReader(s)
			return c.Run()
		}
	}
	return nil
}

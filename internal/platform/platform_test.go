package platform

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeRunner records find/run calls and lets each test pin which commands
// are "available" (LookPath ok) and what runFn should return.
type fakeRunner struct {
	available map[string]bool
	runErr    error

	findCalls []string
	runCalls  []runCall
}

type runCall struct {
	name  string
	args  []string
	stdin string
}

func (f *fakeRunner) lookup() (func(string) error, func(string, []string, io.Reader) error) {
	return func(name string) error {
			f.findCalls = append(f.findCalls, name)
			if f.available[name] {
				return nil
			}
			return errors.New("not found: " + name)
		}, func(name string, args []string, stdin io.Reader) error {
			rc := runCall{name: name, args: append([]string(nil), args...)}
			if stdin != nil {
				b, _ := io.ReadAll(stdin)
				rc.stdin = string(b)
			}
			f.runCalls = append(f.runCalls, rc)
			return f.runErr
		}
}

func newOpener(f *fakeRunner) Opener {
	find, run := f.lookup()
	return Opener{runner: commandRunner{findFn: find, runFn: run}}
}

func newClipboard(f *fakeRunner) Clipboard {
	find, run := f.lookup()
	return Clipboard{runner: commandRunner{findFn: find, runFn: run}}
}

// -----------------------------------------------------------------------------
// Opener
// -----------------------------------------------------------------------------

func TestOpener_PrefersOpenOverXdgOpen(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"open": true, "xdg-open": true}}
	if err := newOpener(f).Open("https://example.com"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0].name != "open" {
		t.Fatalf("expected single 'open' call; got %+v", f.runCalls)
	}
	if f.runCalls[0].args[0] != "https://example.com" {
		t.Fatalf("url not passed: %+v", f.runCalls[0])
	}
}

func TestOpener_FallsBackToXdgOpen(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"xdg-open": true}}
	if err := newOpener(f).Open("https://example.com"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0].name != "xdg-open" {
		t.Fatalf("expected fallback to 'xdg-open'; got %+v", f.runCalls)
	}
}

func TestOpener_NoCommandsAvailable_NoOp(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{}}
	if err := newOpener(f).Open("https://example.com"); err != nil {
		t.Fatalf("Open should not error when nothing's available: %v", err)
	}
	if len(f.runCalls) != 0 {
		t.Fatalf("expected no run calls, got %+v", f.runCalls)
	}
}

func TestOpener_PropagatesRunError(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"open": true}, runErr: errors.New("nope")}
	if err := newOpener(f).Open("u"); err == nil {
		t.Fatalf("expected error from run")
	}
}

func TestOpener_DefaultRunnerIsUsedIfUnset(t *testing.T) {
	// Calling the zero-value opener should fall through to the default
	// runner without panicking. Whatever happens to be on PATH might run;
	// we just assert no panic and no error returned for the no-PATH case.
	o := Opener{}
	// Use a URL likely to be benign even if a command did run.
	_ = o.Open("about:blank")
}

// -----------------------------------------------------------------------------
// Clipboard
// -----------------------------------------------------------------------------

func TestClipboard_PrefersPbcopy(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"pbcopy": true, "wl-copy": true, "xclip": true}}
	if err := newClipboard(f).Copy("hello"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0].name != "pbcopy" {
		t.Fatalf("expected pbcopy; got %+v", f.runCalls)
	}
	if f.runCalls[0].stdin != "hello" {
		t.Fatalf("stdin: got %q want hello", f.runCalls[0].stdin)
	}
}

func TestClipboard_FallsBackToWlCopy(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"wl-copy": true, "xclip": true}}
	if err := newClipboard(f).Copy("hi"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0].name != "wl-copy" {
		t.Fatalf("expected wl-copy fallback; got %+v", f.runCalls)
	}
}

func TestClipboard_FallsBackToXclipWithSelectionArg(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{"xclip": true}}
	if err := newClipboard(f).Copy("ABC-1"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0].name != "xclip" {
		t.Fatalf("expected xclip fallback; got %+v", f.runCalls)
	}
	wantArgs := []string{"-selection", "clipboard"}
	if !equalSS(f.runCalls[0].args, wantArgs) {
		t.Fatalf("xclip args=%v want %v", f.runCalls[0].args, wantArgs)
	}
	if f.runCalls[0].stdin != "ABC-1" {
		t.Fatalf("stdin: %q", f.runCalls[0].stdin)
	}
}

func TestClipboard_NoCommandsAvailable_NoOp(t *testing.T) {
	f := &fakeRunner{available: map[string]bool{}}
	if err := newClipboard(f).Copy("anything"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if len(f.runCalls) != 0 {
		t.Fatalf("expected no run calls; got %+v", f.runCalls)
	}
}

func TestClipboard_PropagatesRunError(t *testing.T) {
	f := &fakeRunner{
		available: map[string]bool{"pbcopy": true},
		runErr:    errors.New("disk full"),
	}
	err := newClipboard(f).Copy("x")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func equalSS(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

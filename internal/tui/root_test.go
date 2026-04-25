package tui_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/tui"
	"github.com/anttti/j/internal/tui/detail"
	"github.com/anttti/j/internal/tui/list"
)

// -----------------------------------------------------------------------------
// fakes
// -----------------------------------------------------------------------------

type fakeFetcher struct {
	calls    []string
	err      error
	upsertFn func(key string) // add to store for successful fetch
}

func (f *fakeFetcher) FetchOne(_ context.Context, key string) error {
	f.calls = append(f.calls, key)
	if f.err != nil {
		return f.err
	}
	if f.upsertFn != nil {
		f.upsertFn(key)
	}
	return nil
}

type fakeOpener struct{ last string }

func (f *fakeOpener) Open(url string) error { f.last = url; return nil }

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

var (
	t0    = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	alice = &model.User{AccountID: "acc-alice", DisplayName: "Alice"}
)

func mkIssue(key, summary string, off time.Duration) model.Issue {
	return model.Issue{
		IssueRef:    model.IssueRef{Key: key, ID: key + "-id", ProjectKey: "ABC"},
		Summary:     summary,
		Type:        "Bug",
		Status:      "To Do",
		Assignee:    alice,
		Created:     t0,
		Updated:     t0.Add(off),
		URL:         "https://x.atlassian.net/browse/" + key,
		Description: "body",
	}
}

func seedStore(t *testing.T, issues ...model.Issue) *memstore.Store {
	t.Helper()
	s := memstore.New()
	ctx := context.Background()
	for _, iss := range issues {
		if err := s.UpsertIssue(ctx, iss, nil); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func drain(m tui.Model, cmd tea.Cmd) tui.Model {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		m, cmd = m.Update(msg)
	}
	return m
}

func loadInto(t *testing.T, m tui.Model) tui.Model {
	t.Helper()
	return drain(m, m.Init())
}

func newApp(t *testing.T, s store.Store, f *fakeFetcher, o *fakeOpener) tui.Model {
	t.Helper()
	return tui.New(tui.Deps{
		Store:    s,
		Fetcher:  f,
		Opener:   o,
		// Clipboard nil is OK.
	})
}

// -----------------------------------------------------------------------------
// tests
// -----------------------------------------------------------------------------

func TestRoot_StartsInListView(t *testing.T) {
	a := mkIssue("ABC-1", "x", 0)
	m := loadInto(t, newApp(t, seedStore(t, a), &fakeFetcher{}, &fakeOpener{}))
	if v := m.CurrentView(); v != tui.ViewList {
		t.Fatalf("view=%v want List", v)
	}
}

func TestRoot_OpenExistingIssue_PushesDetail(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	b := mkIssue("ABC-2", "beta", time.Hour)
	s := seedStore(t, a, b)
	m := loadInto(t, newApp(t, s, &fakeFetcher{}, &fakeOpener{}))

	m, cmd := m.Update(list.OpenIssueMsg{Key: "ABC-1"})
	m = drain(m, cmd)
	if m.CurrentView() != tui.ViewDetail {
		t.Fatalf("view=%v want Detail", m.CurrentView())
	}
	if got := m.DetailKey(); got != "ABC-1" {
		t.Fatalf("detail key=%q want ABC-1", got)
	}
}

// TestRoot_OpenIssue_DisplaysPersistedComments verifies that when the user
// opens an issue, the comments persisted for it are loaded and rendered in
// the detail view. Window size is delivered while the list is focused (the
// realistic startup order); the detail view must inherit that size so the
// comments don't fall off the bottom of the visible page.
func TestRoot_OpenIssue_DisplaysPersistedComments(t *testing.T) {
	// A realistic issue with a multi-line description so that, without size
	// propagation, comments would render below the default 20-line page.
	a := model.Issue{
		IssueRef:    model.IssueRef{Key: "ABC-1", ID: "ABC-1-id", ProjectKey: "ABC"},
		Summary:     "alpha",
		Description: "This is the description.\n\n## Section\n\nMore text here.\n\nLine 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6",
		Type:        "Bug",
		Status:      "To Do",
		Assignee:    alice,
		Created:     t0,
		Updated:     t0,
		URL:         "https://x.atlassian.net/browse/ABC-1",
	}
	s := seedStore(t, a)
	if err := s.ReplaceComments(context.Background(), "ABC-1", []model.Comment{
		{ID: "c1", IssueKey: "ABC-1", Author: alice, Body: "hello from alice", Created: t0},
	}); err != nil {
		t.Fatalf("seed comments: %v", err)
	}

	m := loadInto(t, newApp(t, s, &fakeFetcher{}, &fakeOpener{}))
	// Window size delivered at startup, while list is still focused. This
	// matches Bubble Tea's real boot behavior.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	m, cmd := m.Update(list.OpenIssueMsg{Key: "ABC-1"})
	m = drain(m, cmd)

	if m.CurrentView() != tui.ViewDetail {
		t.Fatalf("view=%v want Detail", m.CurrentView())
	}

	v := m.View()
	if !strings.Contains(v, "Comments (1)") {
		t.Errorf("expected 'Comments (1)' in detail view; got:\n%s", v)
	}
	if !strings.Contains(v, "hello from alice") {
		t.Errorf("expected comment body in detail view; got:\n%s", v)
	}
}

func TestRoot_OpenMissingIssue_FetchesThenPushes(t *testing.T) {
	existing := mkIssue("ABC-1", "alpha", 0)
	s := seedStore(t, existing)
	f := &fakeFetcher{upsertFn: func(key string) {
		_ = s.UpsertIssue(context.Background(),
			mkIssue(key, "remote", time.Hour), nil)
	}}
	m := loadInto(t, newApp(t, s, f, &fakeOpener{}))

	m, cmd := m.Update(list.OpenIssueMsg{Key: "ABC-999"})
	m = drain(m, cmd)
	if len(f.calls) != 1 || f.calls[0] != "ABC-999" {
		t.Fatalf("fetch calls=%v", f.calls)
	}
	if m.CurrentView() != tui.ViewDetail {
		t.Fatalf("view=%v", m.CurrentView())
	}
	if m.DetailKey() != "ABC-999" {
		t.Fatalf("detail key=%q", m.DetailKey())
	}
}

func TestRoot_OpenMissingIssue_FetchError_StaysOnList(t *testing.T) {
	a := mkIssue("ABC-1", "a", 0)
	s := seedStore(t, a)
	f := &fakeFetcher{err: errors.New("boom")}
	m := loadInto(t, newApp(t, s, f, &fakeOpener{}))
	m, cmd := m.Update(list.OpenIssueMsg{Key: "NOPE-9"})
	m = drain(m, cmd)
	if m.CurrentView() != tui.ViewList {
		t.Fatalf("view=%v want List", m.CurrentView())
	}
	if m.Err() == nil {
		t.Fatalf("expected err stored on root")
	}
}

func TestRoot_BackMsg_PopsToList(t *testing.T) {
	a := mkIssue("ABC-1", "a", 0)
	m := loadInto(t, newApp(t, seedStore(t, a), &fakeFetcher{}, &fakeOpener{}))
	m, cmd := m.Update(list.OpenIssueMsg{Key: "ABC-1"})
	m = drain(m, cmd)
	if m.CurrentView() != tui.ViewDetail {
		t.Fatalf("precondition: view=%v", m.CurrentView())
	}
	m, _ = m.Update(detail.BackMsg{})
	if m.CurrentView() != tui.ViewList {
		t.Fatalf("view=%v want List after back", m.CurrentView())
	}
}

func TestRoot_OpenURL_InvokesOpener(t *testing.T) {
	a := mkIssue("ABC-1", "a", 0)
	op := &fakeOpener{}
	m := loadInto(t, newApp(t, seedStore(t, a), &fakeFetcher{}, op))
	m, _ = m.Update(list.OpenURLMsg{URL: "https://x/browse/ABC-1"})
	if op.last != "https://x/browse/ABC-1" {
		t.Fatalf("opener.last=%q", op.last)
	}
	m, _ = m.Update(detail.OpenURLMsg{URL: "https://x/browse/ABC-2"})
	if op.last != "https://x/browse/ABC-2" {
		t.Fatalf("opener.last=%q", op.last)
	}
	_ = m
}

func TestRoot_CtrlC_Quits(t *testing.T) {
	a := mkIssue("ABC-1", "a", 0)
	m := loadInto(t, newApp(t, seedStore(t, a), &fakeFetcher{}, &fakeOpener{}))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("expected Quit cmd")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", msg)
	}
}

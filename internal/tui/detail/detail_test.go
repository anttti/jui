package detail_test

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/tui/detail"
)

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
		Description: "body of " + key + "\n\n## Section\n\nmore text here\n\nline\nline\nline\nline\nline\nline",
		Type:        "Bug",
		Status:      "To Do",
		Assignee:    alice,
		Created:     t0,
		Updated:     t0.Add(off),
		URL:         "https://x.atlassian.net/browse/" + key,
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

func loadInto(t *testing.T, m detail.Model) detail.Model {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next
}

func key(r rune) tea.KeyMsg           { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func special(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func press(t *testing.T, m detail.Model, msgs ...tea.Msg) detail.Model {
	t.Helper()
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next
		for cmd != nil {
			out := cmd()
			if out == nil {
				break
			}
			next2, cmd2 := m.Update(out)
			m = next2
			cmd = cmd2
		}
	}
	return m
}

// -----------------------------------------------------------------------------
// tests
// -----------------------------------------------------------------------------

func TestDetail_InitLoadsComments(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	s := seedStore(t, a)
	_ = s.ReplaceComments(context.Background(), "ABC-1", []model.Comment{
		{ID: "c1", IssueKey: "ABC-1", Body: "first", Created: t0},
		{ID: "c2", IssueKey: "ABC-1", Body: "second", Created: t0.Add(time.Hour)},
	})
	m := loadInto(t, detail.New(s, []model.Issue{a}, 0))
	if got := len(m.Comments()); got != 2 {
		t.Fatalf("comments=%d want 2", got)
	}
	if m.Current().Key != "ABC-1" {
		t.Fatalf("current=%+v", m.Current())
	}
}

func TestDetail_JScrollsDownKScrollsUp(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	m.SetSize(80, 10) // small height so scroll matters
	m = press(t, m, key('j'))
	if m.Scroll() != 1 {
		t.Fatalf("j: scroll=%d want 1", m.Scroll())
	}
	m = press(t, m, key('k'))
	if m.Scroll() != 0 {
		t.Fatalf("k: scroll=%d want 0", m.Scroll())
	}
	// k at top clamps.
	m = press(t, m, key('k'))
	if m.Scroll() != 0 {
		t.Fatalf("k clamp: scroll=%d", m.Scroll())
	}
}

func TestDetail_GGAndG_TopBottom(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	m.SetSize(80, 10)
	m = press(t, m, key('G'))
	if m.Scroll() != m.MaxScroll() {
		t.Fatalf("G: scroll=%d max=%d", m.Scroll(), m.MaxScroll())
	}
	m = press(t, m, key('g'))
	if m.Pending() != "g" {
		t.Fatalf("pending=%q", m.Pending())
	}
	m = press(t, m, key('g'))
	if m.Scroll() != 0 {
		t.Fatalf("gg: scroll=%d", m.Scroll())
	}
}

func TestDetail_HEscQEmitBack(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	cases := []tea.Msg{
		key('h'),
		special(tea.KeyEsc),
		key('q'),
	}
	for _, km := range cases {
		m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
		_, cmd := m.Update(km)
		if cmd == nil {
			t.Fatalf("expected back cmd for %v", km)
		}
		msg := cmd()
		if _, ok := msg.(detail.BackMsg); !ok {
			t.Fatalf("expected BackMsg, got %T for %v", msg, km)
		}
	}
}

func TestDetail_NextPrevIssue(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	b := mkIssue("ABC-2", "beta", time.Hour)
	c := mkIssue("ABC-3", "gamma", 2*time.Hour)
	s := seedStore(t, a, b, c)
	_ = s.ReplaceComments(context.Background(), "ABC-2",
		[]model.Comment{{ID: "cb", IssueKey: "ABC-2", Body: "x", Created: t0}})

	m := loadInto(t, detail.New(s, []model.Issue{a, b, c}, 0))
	m = press(t, m, key(']'))
	if m.Current().Key != "ABC-2" {
		t.Fatalf("after ], current=%s", m.Current().Key)
	}
	// Comments should have reloaded for ABC-2.
	cs := m.Comments()
	if len(cs) != 1 || cs[0].ID != "cb" {
		t.Fatalf("comments after ]: %+v", cs)
	}
	m = press(t, m, key(']'))
	if m.Current().Key != "ABC-3" {
		t.Fatalf("after ]], current=%s", m.Current().Key)
	}
	m = press(t, m, key(']')) // clamp
	if m.Current().Key != "ABC-3" {
		t.Fatalf("clamp ]: current=%s", m.Current().Key)
	}
	m = press(t, m, key('['))
	if m.Current().Key != "ABC-2" {
		t.Fatalf("after [, current=%s", m.Current().Key)
	}
}

func TestDetail_O_EmitsOpenURL(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	_, cmd := m.Update(key('o'))
	if cmd == nil {
		t.Fatalf("expected cmd")
	}
	msg := cmd()
	op, ok := msg.(detail.OpenURLMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	if op.URL != a.URL {
		t.Fatalf("url=%q want %q", op.URL, a.URL)
	}
}

func TestDetail_Y_YanksKey(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	clip := &fakeClipboard{}
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0, detail.WithClipboard(clip)))
	m = press(t, m, key('y'))
	if clip.last != "ABC-1" {
		t.Fatalf("clipboard=%q want ABC-1", clip.last)
	}
}

func TestDetail_View_HasHeaderAndSections(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	s := seedStore(t, a)
	_ = s.ReplaceComments(context.Background(), "ABC-1", []model.Comment{
		{ID: "c1", IssueKey: "ABC-1", Author: alice, Body: "first comment", Created: t0},
	})
	m := loadInto(t, detail.New(s, []model.Issue{a}, 0))
	m.SetSize(100, 40)
	v := m.View()
	if !strings.Contains(v, "ABC-1") {
		t.Errorf("expected issue key in view; got:\n%s", v)
	}
	if !strings.Contains(v, "Description") {
		t.Errorf("expected 'Description' section in view; got:\n%s", v)
	}
	if !strings.Contains(v, "Comments (1)") {
		t.Errorf("expected 'Comments (1)' section in view; got:\n%s", v)
	}
}

// -----------------------------------------------------------------------------
// fake clipboard
// -----------------------------------------------------------------------------

type fakeClipboard struct{ last string }

func (f *fakeClipboard) Copy(s string) error { f.last = s; return nil }

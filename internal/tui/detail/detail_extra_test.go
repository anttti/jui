package detail_test

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/tui/detail"
)

// Tests for the less-traveled key bindings on detail.Model: arrow keys,
// half-page scrolling (Ctrl+D / Ctrl+U), the [ clamp at index 0, and the
// W (open-in-browser) alias.

func TestDetail_ArrowKeys_ScrollByOne(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	m.SetSize(80, 8) // small window so MaxScroll>0
	m = press(t, m, special(tea.KeyDown))
	if m.Scroll() != 1 {
		t.Fatalf("Down: scroll=%d", m.Scroll())
	}
	m = press(t, m, special(tea.KeyUp))
	if m.Scroll() != 0 {
		t.Fatalf("Up: scroll=%d", m.Scroll())
	}
	m = press(t, m, special(tea.KeyUp))
	if m.Scroll() != 0 {
		t.Fatalf("Up clamp: scroll=%d", m.Scroll())
	}
}

func TestDetail_CtrlDAndCtrlU_HalfPageScroll(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	m.SetSize(80, 8)
	max := m.MaxScroll()
	m = press(t, m, special(tea.KeyCtrlD))
	if m.Scroll() == 0 {
		t.Fatalf("Ctrl+D should advance scroll, got 0 (max=%d)", max)
	}
	pos := m.Scroll()
	m = press(t, m, special(tea.KeyCtrlU))
	if m.Scroll() >= pos {
		t.Fatalf("Ctrl+U should reduce scroll: %d -> %d", pos, m.Scroll())
	}
}

func TestDetail_LeftBracket_ClampsAtZero(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	b := mkIssue("ABC-2", "beta", time.Hour)
	m := loadInto(t, detail.New(seedStore(t, a, b), []model.Issue{a, b}, 0))
	m = press(t, m, key('['))
	if m.Current().Key != "ABC-1" {
		t.Fatalf("[ at idx 0 should be no-op; got %q", m.Current().Key)
	}
}

func TestDetail_W_AliasForO(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	_, cmd := m.Update(key('w'))
	if cmd == nil {
		t.Fatalf("expected cmd")
	}
	if _, ok := cmd().(detail.OpenURLMsg); !ok {
		t.Fatalf("expected OpenURLMsg")
	}
}

func TestDetail_New_NegativeIndexClampsToZero(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := detail.New(seedStore(t, a), []model.Issue{a}, -3)
	if m.Current().Key != "ABC-1" {
		t.Fatalf("negative idx should clamp to 0; got %q", m.Current().Key)
	}
}

func TestDetail_New_OutOfRangeClampsToLast(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	b := mkIssue("ABC-2", "beta", time.Hour)
	m := detail.New(seedStore(t, a, b), []model.Issue{a, b}, 99)
	if m.Current().Key != "ABC-2" {
		t.Fatalf("oob idx should clamp to last; got %q", m.Current().Key)
	}
}

func TestDetail_WindowSize_PersistsAndAffectsPageSize(t *testing.T) {
	a := mkIssue("ABC-1", "alpha", 0)
	m := loadInto(t, detail.New(seedStore(t, a), []model.Issue{a}, 0))
	// Tall viewport: MaxScroll trends towards 0 because all content fits.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 1000})
	if next.MaxScroll() != 0 {
		t.Fatalf("with huge height, MaxScroll should be 0; got %d", next.MaxScroll())
	}
}

func TestDetail_CommentsForOtherIssue_AreIgnored(t *testing.T) {
	// loadCommentsCmd is keyed by issue Key. If we receive a stale message
	// targeting a different issue, it should not overwrite current state.
	a := mkIssue("ABC-1", "alpha", 0)
	b := mkIssue("ABC-2", "beta", time.Hour)
	s := seedStore(t, a, b)
	_ = s.ReplaceComments(context.Background(), "ABC-1",
		[]model.Comment{{ID: "c1", IssueKey: "ABC-1", Body: "for a", Created: t0}})
	m := loadInto(t, detail.New(s, []model.Issue{a, b}, 0))
	if got := len(m.Comments()); got != 1 {
		t.Fatalf("precondition: comments=%d", got)
	}
	// Move to ABC-2 — its comments load + replace state.
	m = press(t, m, key(']'))
	if got := len(m.Comments()); got != 0 {
		t.Fatalf("comments for ABC-2 should be empty; got %d", got)
	}
}

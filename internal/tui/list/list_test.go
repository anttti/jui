package list_test

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/tui/list"
)

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

var (
	t0    = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	alice = &model.User{AccountID: "acc-alice", DisplayName: "Alice"}
	bob   = &model.User{AccountID: "acc-bob", DisplayName: "Bob"}
)

func seed(t *testing.T) *memstore.Store {
	t.Helper()
	s := memstore.New()
	ctx := context.Background()
	add := func(key, summary, typ, status string, u *model.User, off time.Duration) {
		iss := model.Issue{
			IssueRef: model.IssueRef{Key: key, ID: key + "-id", ProjectKey: "ABC"},
			Summary:  summary,
			Type:     typ,
			Status:   status,
			Assignee: u,
			Created:  t0,
			Updated:  t0.Add(off),
		}
		if err := s.UpsertIssue(ctx, iss, nil); err != nil {
			t.Fatal(err)
		}
	}
	add("ABC-1", "login flake", "Bug", "To Do", alice, 0)
	add("ABC-2", "metrics dashboard", "Task", "In Progress", bob, time.Hour)
	add("ABC-3", "docs refresh", "Task", "Done", alice, 2*time.Hour)
	return s
}

func loadInto(t *testing.T, m list.Model) list.Model {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
func special(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func press(t *testing.T, m list.Model, msgs ...tea.Msg) list.Model {
	t.Helper()
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next
		// Drain any follow-up reload cmd synchronously.
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

func TestList_InitialLoadPopulatesIssues(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	if got := len(m.Issues()); got != 3 {
		t.Fatalf("issues=%d want 3", got)
	}
	if m.Mode() != list.ModeNormal {
		t.Fatalf("mode=%v want Normal", m.Mode())
	}
	if m.Cursor() != 0 {
		t.Fatalf("cursor=%d want 0", m.Cursor())
	}
}

func TestList_JMovesDownKMovesUp(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('j'))
	if m.Cursor() != 1 {
		t.Fatalf("after j, cursor=%d want 1", m.Cursor())
	}
	m = press(t, m, key('j'))
	if m.Cursor() != 2 {
		t.Fatalf("after jj, cursor=%d want 2", m.Cursor())
	}
	m = press(t, m, key('j')) // should clamp
	if m.Cursor() != 2 {
		t.Fatalf("clamp: cursor=%d want 2", m.Cursor())
	}
	m = press(t, m, key('k'))
	if m.Cursor() != 1 {
		t.Fatalf("after k, cursor=%d want 1", m.Cursor())
	}
}

func TestList_GJumpToBottomAndGG_ToTop(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('G'))
	if m.Cursor() != 2 {
		t.Fatalf("G: cursor=%d want 2", m.Cursor())
	}
	// gg needs two consecutive g presses.
	m = press(t, m, key('g'))
	if m.Pending() != "g" {
		t.Fatalf("after one g, pending=%q want g", m.Pending())
	}
	m = press(t, m, key('g'))
	if m.Cursor() != 0 {
		t.Fatalf("gg: cursor=%d want 0", m.Cursor())
	}
	if m.Pending() != "" {
		t.Fatalf("pending not cleared after gg")
	}
}

func TestList_SlashEntersInsertMode(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('/'))
	if m.Mode() != list.ModeInsert {
		t.Fatalf("mode=%v want Insert", m.Mode())
	}
}

func TestList_InsertModeTypesSearchAndReloads(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('/'))
	for _, r := range "flake" {
		m = press(t, m, key(r))
	}
	if m.SearchBuffer() != "flake" {
		t.Fatalf("buffer=%q", m.SearchBuffer())
	}
	// After each keystroke the list is reloaded with the filtered query.
	// Only ABC-1 matches "flake".
	if len(m.Issues()) != 1 || m.Issues()[0].Key != "ABC-1" {
		t.Fatalf("filtered issues: %v", m.Issues())
	}
}

func TestList_EscInsertClearsSearch(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('/'), key('f'), key('o'), special(tea.KeyEsc))
	if m.Mode() != list.ModeNormal {
		t.Fatalf("mode=%v want Normal", m.Mode())
	}
	if m.SearchBuffer() != "" {
		t.Fatalf("buffer=%q want empty", m.SearchBuffer())
	}
	if len(m.Issues()) != 3 {
		t.Fatalf("issues=%d want 3 after clear", len(m.Issues()))
	}
}

func TestList_EnterInsertKeepsFilterAndReturnsNormal(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('/'), key('f'), key('l'), key('a'), key('k'), key('e'), special(tea.KeyEnter))
	if m.Mode() != list.ModeNormal {
		t.Fatalf("mode=%v want Normal", m.Mode())
	}
	if m.Filter().Search != "flake" {
		t.Fatalf("filter.search=%q", m.Filter().Search)
	}
}

func TestList_ColonEntersCommandMode(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key(':'))
	if m.Mode() != list.ModeCommand {
		t.Fatalf("mode=%v want Command", m.Mode())
	}
}

func TestList_CommandOpenByKey_EmitsOpenMsg(t *testing.T) {
	s := seed(t)
	m := list.New(s)
	m = loadInto(t, m)
	m = press(t, m, key(':'))
	for _, r := range "ABC-2" {
		m = press(t, m, key(r))
	}
	// Capture output of final Enter.
	next, cmd := m.Update(special(tea.KeyEnter))
	m = next
	if cmd == nil {
		t.Fatalf("expected a command from :KEY<Enter>")
	}
	msg := cmd()
	open, ok := msg.(list.OpenIssueMsg)
	if !ok {
		t.Fatalf("expected OpenIssueMsg, got %T", msg)
	}
	if open.Key != "ABC-2" {
		t.Fatalf("key=%q", open.Key)
	}
	if m.Mode() != list.ModeNormal {
		t.Fatalf("mode=%v want Normal after cmd", m.Mode())
	}
}

func TestList_EnterOnRow_EmitsOpenIssueMsg(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	// cursor 0 → ABC-3 (newest updated first).
	next, cmd := m.Update(special(tea.KeyEnter))
	m = next
	if cmd == nil {
		t.Fatalf("expected open cmd")
	}
	msg := cmd()
	op, ok := msg.(list.OpenIssueMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	if op.Key != "ABC-3" {
		t.Fatalf("key=%q want ABC-3", op.Key)
	}
}

func TestList_YY_YanksKey(t *testing.T) {
	s := seed(t)
	clip := &fakeClipboard{}
	m := loadInto(t, list.New(s, list.WithClipboard(clip)))
	m = press(t, m, key('y'))
	if m.Pending() != "y" {
		t.Fatalf("pending=%q want y", m.Pending())
	}
	m = press(t, m, key('y'))
	if m.Pending() != "" {
		t.Fatalf("pending not cleared")
	}
	if clip.last != "ABC-3" {
		t.Fatalf("clipboard=%q want ABC-3", clip.last)
	}
}

func TestList_ChipFocusAndToggle(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('t'))
	if m.ChipFocus() != list.ChipType {
		t.Fatalf("chip=%v want Type", m.ChipFocus())
	}
	// Down to the second type option (Task), toggle with Space.
	m = press(t, m, key('j'))
	m = press(t, m, special(tea.KeySpace))
	m = press(t, m, special(tea.KeyEnter)) // commit selection, return to list
	if m.ChipFocus() != list.ChipNone {
		t.Fatalf("chip should unfocus after Enter")
	}
	fs := m.Filter()
	if len(fs.Types) == 0 {
		t.Fatalf("expected type filter to be set, got %+v", fs.Types)
	}
}

// -----------------------------------------------------------------------------
// fake clipboard
// -----------------------------------------------------------------------------

type fakeClipboard struct{ last string }

func (f *fakeClipboard) Copy(s string) error { f.last = s; return nil }

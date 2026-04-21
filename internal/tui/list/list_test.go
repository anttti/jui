package list_test

import (
	"context"
	"strings"
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

func TestList_ChipType_DropdownShowsAllValues(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('t'))
	v := m.View()
	// Seed has types Bug and Task; dropdown renders them with checkboxes.
	if !strings.Contains(v, "[ ] Bug") {
		t.Errorf("expected '[ ] Bug' checkbox line; got:\n%s", v)
	}
	if !strings.Contains(v, "[ ] Task") {
		t.Errorf("expected '[ ] Task' checkbox line; got:\n%s", v)
	}
}

func TestList_ChipType_SpaceTogglesCheckbox(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('t')) // focus type chip, cursor at Bug (sorted first)
	m = press(t, m, special(tea.KeySpace))
	v := m.View()
	if !strings.Contains(v, "[x] Bug") {
		t.Errorf("expected '[x] Bug' after Space; got:\n%s", v)
	}
}

func TestList_ChipType_JMovesDropdownCursor(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('t'))
	// cursor on Bug; j moves to Task; Space selects Task only.
	m = press(t, m, key('j'))
	m = press(t, m, special(tea.KeySpace))
	m = press(t, m, special(tea.KeyEnter))
	fs := m.Filter()
	if len(fs.Types) != 1 || fs.Types[0] != "Task" {
		t.Fatalf("expected Types=[Task]; got %+v", fs.Types)
	}
}

func TestList_ChipType_RefocusPreselectsCurrentFilter(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	// Select Task and commit.
	m = press(t, m, key('t'))
	m = press(t, m, key('j'))
	m = press(t, m, special(tea.KeySpace))
	m = press(t, m, special(tea.KeyEnter))
	// Re-focus: the current filter should be reflected as already-checked.
	m = press(t, m, key('t'))
	v := m.View()
	if !strings.Contains(v, "[x] Task") {
		t.Errorf("re-focused chip should show '[x] Task'; got:\n%s", v)
	}
}

func TestList_ChipStatus_DropdownShowsAllValues(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('s'))
	v := m.View()
	for _, want := range []string{"[ ] To Do", "[ ] In Progress", "[ ] Done"} {
		if !strings.Contains(v, want) {
			t.Errorf("expected %q; got:\n%s", want, v)
		}
	}
}

func TestList_ChipAssignee_DropdownIncludesAllAndUsers(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('a'))
	v := m.View()
	// Dropdown rows are marked with "• " so they're distinguishable from
	// the chip summary label.
	if !strings.Contains(v, "• All") {
		t.Errorf("expected '• All' entry; got:\n%s", v)
	}
	if !strings.Contains(v, "• Alice") {
		t.Errorf("expected '• Alice' entry; got:\n%s", v)
	}
	if !strings.Contains(v, "• Bob") {
		t.Errorf("expected '• Bob' entry; got:\n%s", v)
	}
}

func TestList_ChipAssignee_SelectingUserFiltersList(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('a'))
	// Dropdown starts with "All" at the top; j moves to Alice (sorted).
	m = press(t, m, key('j'))
	m = press(t, m, special(tea.KeyEnter))
	fs := m.Filter()
	if fs.Assignee.Kind != model.AssigneeKindAccount || fs.Assignee.AccountID != "acc-alice" {
		t.Fatalf("expected Assignee=acc-alice; got %+v", fs.Assignee)
	}
	// Only Alice's issues should be listed (ABC-1, ABC-3).
	if len(m.Issues()) != 2 {
		t.Fatalf("expected 2 issues after filter; got %d", len(m.Issues()))
	}
	for _, iss := range m.Issues() {
		if iss.Assignee == nil || iss.Assignee.AccountID != "acc-alice" {
			t.Fatalf("unexpected issue %q in filtered results", iss.Key)
		}
	}
}

func TestList_ChipAssignee_AllOptionClearsFilter(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	// First set a specific assignee.
	m = press(t, m, key('a'))
	m = press(t, m, key('j')) // Alice
	m = press(t, m, special(tea.KeyEnter))
	// Now re-open and pick the first row (All).
	m = press(t, m, key('a'))
	m = press(t, m, special(tea.KeyEnter))
	fs := m.Filter()
	if fs.Assignee.Kind != model.AssigneeKindAll {
		t.Fatalf("expected Assignee=All; got %+v", fs.Assignee)
	}
	if len(m.Issues()) != 3 {
		t.Fatalf("expected 3 issues after clearing; got %d", len(m.Issues()))
	}
}

func TestList_View_HasColumnHeader(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	v := m.View()
	for _, want := range []string{"KEY", "STATUS", "PRIO", "ASG", "SUMMARY"} {
		if !strings.Contains(v, want) {
			t.Errorf("expected column header %q in view; got:\n%s", want, v)
		}
	}
}

func TestList_QKeyQuits(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	_, cmd := m.Update(key('q'))
	if cmd == nil {
		t.Fatalf("expected quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
	}
}

func TestList_WKeyOpensURL(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	_, cmd := m.Update(key('w'))
	if cmd == nil {
		t.Fatalf("expected open-url cmd")
	}
	msg := cmd()
	if _, ok := msg.(list.OpenURLMsg); !ok {
		t.Fatalf("expected OpenURLMsg, got %T", msg)
	}
}

func TestList_SortChip_SpaceTogglesAscDescOff(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	m = press(t, m, key('z'))
	if m.ChipFocus() != list.ChipSort {
		t.Fatalf("chip=%v want Sort", m.ChipFocus())
	}
	// Focused on first sortable column (KEY). Space → asc.
	m = press(t, m, special(tea.KeySpace))
	if got := m.Sort(); len(got) != 1 || got[0].Column != list.ColKey || got[0].Desc {
		t.Fatalf("after first space: sort=%+v", got)
	}
	// Second space → desc.
	m = press(t, m, special(tea.KeySpace))
	if got := m.Sort(); len(got) != 1 || got[0].Column != list.ColKey || !got[0].Desc {
		t.Fatalf("after second space: sort=%+v", got)
	}
	// Third space → removed.
	m = press(t, m, special(tea.KeySpace))
	if got := m.Sort(); len(got) != 0 {
		t.Fatalf("after third space: sort=%+v", got)
	}
}

func TestList_SortChip_SortsIssues(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	// Sort by KEY ascending. Default order is by Updated desc
	// (ABC-3, ABC-2, ABC-1); after KEY-asc it should be ABC-1, ABC-2, ABC-3.
	m = press(t, m, key('z'))
	m = press(t, m, special(tea.KeySpace))
	m = press(t, m, special(tea.KeyEnter))
	issues := m.Issues()
	if len(issues) != 3 || issues[0].Key != "ABC-1" || issues[2].Key != "ABC-3" {
		t.Fatalf("sort order wrong: %v %v %v", issues[0].Key, issues[1].Key, issues[2].Key)
	}
}

func TestList_PresetSaveAndRecall(t *testing.T) {
	s := seed(t)
	m := loadInto(t, list.New(s))
	// Start with default columns. Reduce to 3 via the column chip.
	m = press(t, m, key('c'))
	if m.ChipFocus() != list.ChipColumns {
		t.Fatalf("chip=%v want Columns", m.ChipFocus())
	}
	// The working order starts with DefaultColumns (7 items) followed by any
	// extras. Hide the last 4 visible defaults by space-toggling.
	// DefaultColumns: key, type, status, prio, assignee, summary, updated.
	// Move to index 3 (prio) and toggle off 4 columns.
	for i := 0; i < 3; i++ {
		m = press(t, m, key('j'))
	}
	for i := 0; i < 4; i++ {
		m = press(t, m, special(tea.KeySpace))
		m = press(t, m, key('j'))
	}
	m = press(t, m, special(tea.KeyEnter))
	got := m.Columns()
	if len(got) != 3 {
		t.Fatalf("after column config: %v", got)
	}
	// Save to preset 4.
	m = press(t, m, key('p'))
	if m.ChipFocus() != list.ChipPresetSave {
		t.Fatalf("chip=%v want PresetSave", m.ChipFocus())
	}
	m = press(t, m, key('4'))
	if m.ActivePreset() != 4 {
		t.Fatalf("active=%d want 4", m.ActivePreset())
	}
	// Recall preset 4 later — first clobber the live columns, then press 4.
	m = press(t, m, key('c'))
	m = press(t, m, special(tea.KeyEsc)) // no change, just ensure we're normal
	m = press(t, m, key('4'))
	if m.ActivePreset() != 4 {
		t.Fatalf("after recall: active=%d", m.ActivePreset())
	}
	if len(m.Columns()) != 3 {
		t.Fatalf("after recall columns=%v", m.Columns())
	}
}

// -----------------------------------------------------------------------------
// fake clipboard
// -----------------------------------------------------------------------------

type fakeClipboard struct{ last string }

func (f *fakeClipboard) Copy(s string) error { f.last = s; return nil }

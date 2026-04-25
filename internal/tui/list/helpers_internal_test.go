package list

import (
	"testing"
	"time"

	"github.com/anttti/j/internal/model"
)

// These are internal tests for the unexported helpers in list.go. They
// would otherwise stay 0% — the public Update flow rarely takes the rare
// branches (large windows, distant timestamps, malformed issue keys).

func TestRelTime_FormatsRanges(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, "—"},
		{"now", now, "now"},
		{"few seconds ago is still 'now'", now.Add(-5 * time.Second), "now"},
		{"5 minutes", now.Add(-5 * time.Minute), "5m"},
		{"3 hours", now.Add(-3 * time.Hour), "3h"},
		{"3 days", now.Add(-3 * 24 * time.Hour), "3d"},
		{"2 weeks", now.Add(-15 * 24 * time.Hour), "2w"},
		{"2 years", now.Add(-2 * 365 * 24 * time.Hour), "2y"},
		// Future stamps fold to absolute durations.
		{"future 5m", now.Add(5*time.Minute + time.Second), "5m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relTime(tc.t); got != tc.want {
				t.Fatalf("relTime: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestLooksLikeIssueKey(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ABC-1", true},
		{"abc-1", true},
		{"AB12-99", true},
		{"ABC-", false},
		{"-1", false},
		{"ABC", false},
		{"ABC-12X", false},
		{"AB!C-1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeIssueKey(tc.in); got != tc.want {
			t.Errorf("looksLikeIssueKey(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestHalfPage_DefaultsAndMath(t *testing.T) {
	if got := halfPage(0); got != 10 {
		t.Fatalf("halfPage(0)=%d want default 10", got)
	}
	// (h-6)/2
	if got := halfPage(26); got != 10 {
		t.Fatalf("halfPage(26)=%d want 10", got)
	}
	if got := halfPage(20); got != 7 {
		t.Fatalf("halfPage(20)=%d want 7", got)
	}
}

func TestPageSize_DefaultsAndMath(t *testing.T) {
	if got := pageSize(0); got != 20 {
		t.Fatalf("pageSize(0)=%d want default 20", got)
	}
	if got := pageSize(30); got != 24 {
		t.Fatalf("pageSize(30)=%d want 24", got)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{15, 0, 10, 10},
		{5, 10, 0, 10}, // hi<lo: return lo
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%d,%d,%d)=%d want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

func TestMaxInt(t *testing.T) {
	if maxInt(2, 3) != 3 {
		t.Fatal("maxInt(2,3) want 3")
	}
	if maxInt(7, -1) != 7 {
		t.Fatal("maxInt(7,-1) want 7")
	}
}

func TestSummariseSelected(t *testing.T) {
	if got := summariseSelected(map[string]bool{}); got != "All" {
		t.Fatalf("empty -> %q want All", got)
	}
	one := summariseSelected(map[string]bool{"Bug": true})
	if one != "Bug" {
		t.Fatalf("single -> %q", one)
	}
	two := summariseSelected(map[string]bool{"Bug": true, "Task": true})
	if two != "Bug, Task" {
		t.Fatalf("two -> %q", two)
	}
	many := summariseSelected(map[string]bool{"Bug": true, "Task": true, "Story": true, "Epic": true})
	// >2 truncates to "first, +N".
	if got := many; got == "" || got[len(got)-3:] != " +3" {
		t.Fatalf("many -> %q want '<first>, +3'", got)
	}
	// keys with false values are filtered.
	if got := summariseSelected(map[string]bool{"Bug": false}); got != "All" {
		t.Fatalf("false-valued key should be ignored, got %q", got)
	}
}

func TestSummariseSort(t *testing.T) {
	if got := summariseSort(nil); got != "none" {
		t.Fatalf("empty sort = %q want 'none'", got)
	}
	if got := summariseSort([]SortKey{{Column: ColKey, Desc: false}}); !endsWith(got, "↑") {
		t.Fatalf("asc should end in ↑: %q", got)
	}
	if got := summariseSort([]SortKey{{Column: ColKey, Desc: true}}); !endsWith(got, "↓") {
		t.Fatalf("desc should end in ↓: %q", got)
	}
	multi := summariseSort([]SortKey{
		{Column: ColPrio, Desc: false},
		{Column: ColUpdated, Desc: true},
	})
	if multi == "" {
		t.Fatalf("multi should not be empty")
	}
	// Multi formatted with ", " separator.
	if !contains(multi, ", ") {
		t.Fatalf("expected ', ' separator in multi: %q", multi)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestSummariseCols(t *testing.T) {
	cols := []ColumnID{ColKey, ColSummary}
	if got := summariseCols(cols, 0); got != "2" {
		t.Fatalf("no preset: got %q want '2'", got)
	}
	if got := summariseCols(cols, 4); got != "preset 4 (2)" {
		t.Fatalf("with preset: got %q", got)
	}
}

func TestAssigneeLabel(t *testing.T) {
	cases := []struct {
		f    model.AssigneeFilter
		want string
	}{
		{model.AssigneeAll(), "All"},
		{model.AssigneeMe(), "Me"},
		{model.AssigneeUnassigned(), "Unassigned"},
		{model.AssigneeAccount(""), "All"},
		{model.AssigneeAccount("acc-42"), "acc-42"},
	}
	for _, c := range cases {
		if got := assigneeLabel(c.f); got != c.want {
			t.Errorf("assigneeLabel(%+v)=%q want %q", c.f, got, c.want)
		}
	}
}

func TestKeysTrue_FiltersFalseValues(t *testing.T) {
	in := map[string]bool{"a": true, "b": false, "c": true}
	got := keysTrue(in)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (%v)", len(got), got)
	}
	for _, k := range got {
		if k != "a" && k != "c" {
			t.Errorf("unexpected key %q", k)
		}
	}
}

func TestContainsCol(t *testing.T) {
	cols := []ColumnID{ColKey, ColStatus}
	if !containsCol(cols, ColKey) {
		t.Fatal("expected true for present column")
	}
	if containsCol(cols, ColAssignee) {
		t.Fatal("expected false for absent column")
	}
}

// -----------------------------------------------------------------------------
// columns.go internal helpers
// -----------------------------------------------------------------------------

func TestTruncateEllipsis(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"hi", 0, ""},
		{"hi", -1, ""},
		{"hi", 1, "…"},
	}
	for _, c := range cases {
		if got := truncateEllipsis(c.in, c.w); got != c.want {
			t.Errorf("truncateEllipsis(%q, %d) = %q want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestComputeColumnWidths_FlexAbsorbsRemainder(t *testing.T) {
	cols := []ColumnID{ColKey, ColSummary, ColUpdated}
	widths := computeColumnWidths(cols, 50)
	if len(widths) != 3 {
		t.Fatalf("widths=%v", widths)
	}
	// KEY=10, UPD=6, summary is the flex column → fills remainder.
	if widths[0] != 10 || widths[2] != 6 {
		t.Fatalf("fixed widths off: %v", widths)
	}
	// 50 - (10 + 6) - 2 (separators between 3 cols) = 32
	if widths[1] != 32 {
		t.Fatalf("flex width: %d want 32 (widths=%v)", widths[1], widths)
	}
}

func TestComputeColumnWidths_FlexClampsToZero(t *testing.T) {
	cols := []ColumnID{ColKey, ColSummary, ColUpdated}
	widths := computeColumnWidths(cols, 5) // way too narrow
	if widths[1] != 0 {
		t.Fatalf("flex should clamp to 0 when negative; got %v", widths)
	}
}

func TestColumnDef_UnknownReturnsKey(t *testing.T) {
	cd := columnDef("not-a-column")
	if cd.id != ColKey {
		t.Fatalf("unknown column id should fall back to ColKey, got %v", cd.id)
	}
}

func TestColumnLabel(t *testing.T) {
	if got := columnLabel(ColKey); got != "KEY" {
		t.Fatalf("columnLabel(ColKey)=%q want KEY", got)
	}
}

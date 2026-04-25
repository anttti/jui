package style_test

import (
	"strings"
	"testing"

	"github.com/anttti/j/internal/tui/style"
)

func TestInitials(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "?"},
		{"whitespace", "   ", "?"},
		{"single name", "Alice", "A"},
		{"first last", "Alice Wonder", "AW"},
		{"three names uses first+last", "Alice B Wonder", "AW"},
		{"lowercase upcased", "alice wonder", "AW"},
		{"non-letter prefix skipped", "@alice", "A"},
		{"digits ok", "1stPlace", "1"},
		{"all symbols", "***", "?"},
		{"hyphenated last name kept whole", "Mary Jane-Smith", "MJ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := style.Initials(tc.in); got != tc.want {
				t.Fatalf("Initials(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAssigneeColor_StableForSameID(t *testing.T) {
	a := style.AssigneeColor("acc-1")
	b := style.AssigneeColor("acc-1")
	if a != b {
		t.Fatalf("expected stable color, got %v vs %v", a, b)
	}
}

func TestAssigneeColor_EmptyIDReturnsMutedColor(t *testing.T) {
	c := style.AssigneeColor("")
	if c == "" {
		t.Fatalf("expected non-empty color for empty id")
	}
}

func TestAssigneeColor_DifferentIDsCoverPalette(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[string(style.AssigneeColor(string(rune('a'+i%26))+string(rune('A'+i%26))+string(rune('0'+i%10))))] = true
	}
	if len(seen) < 4 {
		t.Fatalf("palette should yield several distinct colors; saw %d", len(seen))
	}
}

func TestStatusStyle_PicksColorByCategory(t *testing.T) {
	done := style.StatusStyle("done").GetForeground()
	indet := style.StatusStyle("indeterminate").GetForeground()
	todo := style.StatusStyle("todo").GetForeground()
	other := style.StatusStyle("anything-else").GetForeground()
	if done == indet || done == todo || done == other {
		t.Fatalf("done should differ from other categories: %v %v %v %v", done, indet, todo, other)
	}
	if got := style.StatusStyle("new").GetForeground(); got != todo {
		t.Fatalf("'new' should be styled like 'todo': %v vs %v", got, todo)
	}
	if got := style.StatusStyle("DONE").GetForeground(); got != done {
		t.Fatalf("status style should be case-insensitive")
	}
}

func TestPriorityStyle_PicksColorByName(t *testing.T) {
	high := style.PriorityStyle("High").GetForeground()
	highest := style.PriorityStyle("Highest").GetForeground()
	medium := style.PriorityStyle("Medium").GetForeground()
	low := style.PriorityStyle("Low").GetForeground()
	other := style.PriorityStyle("???").GetForeground()
	if high != highest {
		t.Fatalf("high and highest should share style: %v vs %v", high, highest)
	}
	if high == medium || high == low || high == other {
		t.Fatalf("high should differ from medium/low/other: %v %v %v %v", high, medium, low, other)
	}
	if got := style.PriorityStyle("LOWEST").GetForeground(); got != low {
		t.Fatalf("priority should be case-insensitive")
	}
}

func TestTypeStyle_PicksColorByName(t *testing.T) {
	bug := style.TypeStyle("Bug").GetForeground()
	story := style.TypeStyle("Story").GetForeground()
	epic := style.TypeStyle("Epic").GetForeground()
	task := style.TypeStyle("Task").GetForeground()
	subtask := style.TypeStyle("Sub-task").GetForeground()
	other := style.TypeStyle("Custom").GetForeground()
	if bug == story || story == epic || epic == task || task == other {
		t.Fatalf("each type should have distinct style")
	}
	if task != subtask {
		t.Fatalf("task and sub-task should share style")
	}
	if got := style.TypeStyle("subtask").GetForeground(); got != subtask {
		t.Fatalf("'subtask' should equal 'sub-task'")
	}
}

func TestTypeSymbol(t *testing.T) {
	cases := map[string]string{
		"Bug":      "■",
		"Story":    "◆",
		"Epic":     "★",
		"Task":     "▲",
		"Sub-task": "↳",
		"subtask":  "↳",
		"unknown":  "●",
		"":         "●",
	}
	for in, want := range cases {
		if got := style.TypeSymbol(in); got != want {
			t.Errorf("TypeSymbol(%q)=%q want %q", in, got, want)
		}
	}
}

func TestModePill_RendersLabelText(t *testing.T) {
	for _, mode := range []string{"NORMAL", "INSERT", "COMMAND", "OTHER"} {
		out := style.ModePill(mode)
		if !strings.Contains(out, mode) {
			t.Fatalf("ModePill(%q) missing label text in: %q", mode, out)
		}
	}
}

func TestHorizontalRule_WidthClampsToOne(t *testing.T) {
	if got := style.HorizontalRule(0); !strings.Contains(got, "─") {
		t.Fatalf("HorizontalRule(0) should still render at least one rune: %q", got)
	}
	if got := style.HorizontalRule(-5); !strings.Contains(got, "─") {
		t.Fatalf("HorizontalRule(-5) should still render at least one rune: %q", got)
	}
}

func TestHorizontalRule_WidthEqualsRequested(t *testing.T) {
	out := style.HorizontalRule(8)
	if strings.Count(out, "─") != 8 {
		t.Fatalf("HorizontalRule(8) should contain 8 runes; got %q", out)
	}
}

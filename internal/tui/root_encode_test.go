package tui

import (
	"reflect"
	"testing"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/tui/appstate"
	"github.com/anttti/j/internal/tui/list"
)

// These tests exercise the persistence-layer encode/decode helpers in
// root.go. They are unexported but live in the same package, so we use an
// internal _test.go (same package) to drive them. Each test asserts a
// round-trip preserves the value.

func TestEncodeDecodeFilter_RoundTripsAllAssigneeKinds(t *testing.T) {
	cases := []struct {
		name string
		in   model.Filter
	}{
		{
			"all",
			model.Filter{
				Types:    []string{"Bug", "Task"},
				Statuses: []string{"To Do"},
				Assignee: model.AssigneeAll(),
				Search:   "flake",
			},
		},
		{
			"me",
			model.Filter{
				Types:    nil,
				Statuses: nil,
				Assignee: model.AssigneeMe(),
			},
		},
		{
			"unassigned",
			model.Filter{
				Assignee: model.AssigneeUnassigned(),
			},
		},
		{
			"account",
			model.Filter{
				Assignee: model.AssigneeAccount("acc-42"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := decodeFilter(encodeFilter(tc.in))
			if !reflect.DeepEqual(normalizeFilter(out), normalizeFilter(tc.in)) {
				t.Fatalf("round trip mismatch:\n got: %+v\nwant: %+v", out, tc.in)
			}
		})
	}
}

func TestDecodeFilter_UnknownAssigneeKindFallsBackToAll(t *testing.T) {
	got := decodeFilter(appstate.Filter{Assignee: appstate.Assignee{Kind: "weird"}})
	if got.Assignee.Kind != model.AssigneeKindAll {
		t.Fatalf("kind=%v want All", got.Assignee.Kind)
	}
}

func TestEncodeDecodeSort_RoundTrip(t *testing.T) {
	in := []list.SortKey{
		{Column: list.ColPrio, Desc: false},
		{Column: list.ColUpdated, Desc: true},
	}
	got := decodeSort(encodeSort(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestEncodeSort_NilForEmpty(t *testing.T) {
	if got := encodeSort(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := decodeSort(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestEncodeDecodeColumns_RoundTrip(t *testing.T) {
	in := []list.ColumnID{list.ColKey, list.ColStatus, list.ColSummary}
	got := decodeColumns(encodeColumns(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestEncodeDecodeColumns_NilForEmpty(t *testing.T) {
	if got := encodeColumns(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := decodeColumns(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestEncodeDecodePresets_RoundTrip(t *testing.T) {
	in := map[int][]list.ColumnID{
		1: {list.ColKey, list.ColSummary},
		3: {list.ColKey, list.ColStatus, list.ColPrio},
	}
	got := decodePresets(encodePresets(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestEncodePresets_NilForEmpty(t *testing.T) {
	if got := encodePresets(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := decodePresets(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDecodePresets_SkipsNonIntegerSlots(t *testing.T) {
	in := map[string][]string{
		"1":   {"key"},
		"abc": {"summary"}, // bogus slot, must be skipped
		"2":   {"status"},
	}
	got := decodePresets(in)
	if _, ok := got[1]; !ok {
		t.Fatalf("missing slot 1: %+v", got)
	}
	if _, ok := got[2]; !ok {
		t.Fatalf("missing slot 2: %+v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 valid slots, got %d: %+v", len(got), got)
	}
}

// normalizeFilter swaps nil/empty slice variants so reflect.DeepEqual ignores
// the difference: encodeFilter produces empty slices, model.Filter often has
// nil. We compare the meaningful identity of the value.
func normalizeFilter(f model.Filter) model.Filter {
	if len(f.Types) == 0 {
		f.Types = nil
	}
	if len(f.Statuses) == 0 {
		f.Statuses = nil
	}
	return f
}

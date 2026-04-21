package appstate_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/anttti/j/internal/tui/appstate"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	want := appstate.State{
		View:        "detail",
		SelectedKey: "ABC-42",
		Filter: appstate.Filter{
			Types:    []string{"Bug"},
			Statuses: []string{"To Do"},
			Assignee: appstate.Assignee{Kind: "account", AccountID: "acc-1"},
			Search:   "flake",
		},
		Sort:         []appstate.SortKey{{Column: "key", Desc: true}},
		Columns:      []string{"key", "status"},
		ActivePreset: 3,
		Presets: map[string][]string{
			"1": {"key", "summary"},
			"3": {"key", "status"},
		},
	}
	if err := appstate.Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := appstate.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLoadMissingFileReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	s, err := appstate.Load(path)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if !reflect.DeepEqual(s, appstate.State{}) {
		t.Fatalf("expected zero State, got %+v", s)
	}
}

func TestSaveEmptyPathIsNoop(t *testing.T) {
	if err := appstate.Save("", appstate.State{}); err != nil {
		t.Fatalf("save('') should be a noop, got: %v", err)
	}
}

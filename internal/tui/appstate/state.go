// Package appstate is the on-disk shape of persistent UI state: active view,
// selected issue, filter, sort, and column presets. Kept free of tui imports
// so it can be loaded by the root and distributed to leaf views without
// creating cycles.
package appstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is the persisted shape of the app's UI state between launches.
type State struct {
	View         string              `json:"view"`
	SelectedKey  string              `json:"selected_key,omitempty"`
	Filter       Filter              `json:"filter"`
	Sort         []SortKey           `json:"sort,omitempty"`
	Columns      []string            `json:"columns,omitempty"`
	ActivePreset int                 `json:"active_preset,omitempty"`
	Presets      map[string][]string `json:"presets,omitempty"`
}

// Filter is a serializable projection of model.Filter.
type Filter struct {
	Types    []string `json:"types,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	Assignee Assignee `json:"assignee"`
	Search   string   `json:"search,omitempty"`
}

// Assignee is a serializable projection of model.AssigneeFilter.
type Assignee struct {
	Kind      string `json:"kind"` // all|me|unassigned|account
	AccountID string `json:"account_id,omitempty"`
}

// SortKey is a serializable projection of list.SortKey.
type SortKey struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc"`
}

// Load reads state from path. A missing file returns the zero State with a
// nil error.
func Load(path string) (State, error) {
	if path == "" {
		return State{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Save writes state atomically to path. An empty path is a no-op.
func Save(path string, s State) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

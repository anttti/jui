// Package memstore is an in-memory implementation of store.Store intended
// for tests and UI prototyping.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/anttti/j/internal/model"
)

type Store struct {
	mu       sync.RWMutex
	issues   map[string]model.Issue   // key: upper-case key
	comments map[string][]model.Comment
	sync     model.SyncState
}

// New returns a fresh empty store.
func New() *Store {
	return &Store{
		issues:   make(map[string]model.Issue),
		comments: make(map[string][]model.Comment),
	}
}

func (s *Store) Close() error { return nil }

func normKey(k string) string { return strings.ToUpper(k) }

// ---------- Reader ----------

func (s *Store) Get(_ context.Context, key string) (*model.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	iss, ok := s.issues[normKey(key)]
	if !ok {
		return nil, nil
	}
	return &iss, nil
}

func (s *Store) List(_ context.Context, f model.Filter, p model.Page) ([]model.Issue, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]model.Issue, 0, len(s.issues))
	for _, iss := range s.issues {
		if matches(iss, f) {
			matched = append(matched, iss)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Updated.After(matched[j].Updated)
	})
	total := len(matched)
	if p.Offset > 0 {
		if p.Offset >= len(matched) {
			matched = nil
		} else {
			matched = matched[p.Offset:]
		}
	}
	if p.Limit > 0 && len(matched) > p.Limit {
		matched = matched[:p.Limit]
	}
	out := append([]model.Issue(nil), matched...)
	return out, total, nil
}

func (s *Store) Comments(_ context.Context, key string) ([]model.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cs := s.comments[normKey(key)]
	if len(cs) == 0 {
		return nil, nil
	}
	out := append([]model.Comment(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

func (s *Store) DistinctTypes(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, iss := range s.issues {
		if iss.Type != "" {
			seen[iss.Type] = struct{}{}
		}
	}
	return sortedKeys(seen), nil
}

func (s *Store) DistinctStatuses(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, iss := range s.issues {
		if iss.Status != "" {
			seen[iss.Status] = struct{}{}
		}
	}
	return sortedKeys(seen), nil
}

func (s *Store) Assignees(_ context.Context) ([]model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]model.User{}
	for _, iss := range s.issues {
		if iss.Assignee == nil {
			continue
		}
		if _, ok := seen[iss.Assignee.AccountID]; !ok {
			seen[iss.Assignee.AccountID] = *iss.Assignee
		}
	}
	out := make([]model.User, 0, len(seen))
	for _, u := range seen {
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out, nil
}

func (s *Store) SyncState(_ context.Context) (model.SyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sync, nil
}

// ---------- Writer ----------

func (s *Store) UpsertIssue(_ context.Context, iss model.Issue, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues[normKey(iss.Key)] = iss
	return nil
}

func (s *Store) ReplaceComments(_ context.Context, issueKey string, cs []model.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]model.Comment(nil), cs...)
	s.comments[normKey(issueKey)] = cp
	return nil
}

func (s *Store) SetSyncState(_ context.Context, ss model.SyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sync = ss
	return nil
}

// ---------- internals ----------

func matches(iss model.Issue, f model.Filter) bool {
	if len(f.Types) > 0 && !contains(f.Types, iss.Type) {
		return false
	}
	if len(f.Statuses) > 0 && !contains(f.Statuses, iss.Status) {
		return false
	}
	switch f.Assignee.Kind {
	case model.AssigneeKindUnassigned:
		if iss.Assignee != nil {
			return false
		}
	case model.AssigneeKindAccount:
		if iss.Assignee == nil || iss.Assignee.AccountID != f.Assignee.AccountID {
			return false
		}
	case model.AssigneeKindMe:
		// Resolution of "me" happens in the caller; if we see it here we
		// treat it as "no match" to make the bug obvious in tests.
		return false
	}
	if f.Search != "" {
		needle := strings.ToLower(f.Search)
		if !strings.Contains(strings.ToLower(iss.Summary), needle) &&
			!strings.Contains(strings.ToLower(iss.Description), needle) {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

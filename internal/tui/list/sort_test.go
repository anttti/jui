package list_test

import (
	"context"
	"testing"
	"time"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store/memstore"
	"github.com/anttti/j/internal/tui/list"
)

// sortBy seeds a memstore with the issues, opens a list.Model with the given
// sort key(s) preloaded, and returns the sorted Issues() slice. This drives
// applySort + compareByColumn through the only path the package exposes.
func sortBy(t *testing.T, issues []model.Issue, keys ...list.SortKey) []model.Issue {
	t.Helper()
	s := memstore.New()
	for _, iss := range issues {
		if err := s.UpsertIssue(context.Background(), iss, nil); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	m := list.New(s, list.WithInitialState(nil, keys, nil, 0, model.Filter{}))
	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("expected initial reload cmd")
	}
	next, _ := m.Update(cmd())
	return next.Issues()
}

func mkSortIssue(key, typ, status, prio, summary string, asg *model.User, created, updated time.Time) model.Issue {
	return model.Issue{
		IssueRef: model.IssueRef{Key: key, ID: key + "-id", ProjectKey: "ABC"},
		Summary:  summary,
		Type:     typ,
		Status:   status,
		Priority: prio,
		Assignee: asg,
		Created:  created,
		Updated:  updated,
	}
}

// applySort is an internal helper, but we exercise it through the public
// list.Model — sort goes through Update on a loaded model. This indirectly
// covers compareByColumn, cmpString, cmpInt, priorityRank, assigneeName.
func TestSort_ByPriority_ranksHighestFirstWhenAscending(t *testing.T) {
	now := time.Now()
	a := mkSortIssue("ABC-1", "Bug", "To Do", "Medium", "summary", nil, now, now)
	b := mkSortIssue("ABC-2", "Bug", "To Do", "Highest", "summary", nil, now, now)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "Low", "summary", nil, now, now)
	d := mkSortIssue("ABC-4", "Bug", "To Do", "", "summary", nil, now, now)

	got := sortBy(t, []model.Issue{a, b, c, d}, list.SortKey{Column: list.ColPrio, Desc: false})
	wantOrder := []string{"ABC-2", "ABC-1", "ABC-3", "ABC-4"} // Highest, Medium, Low, blank
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("sort by prio asc: pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_ByPriority_descReversesOrder(t *testing.T) {
	now := time.Now()
	a := mkSortIssue("ABC-1", "Bug", "To Do", "Medium", "x", nil, now, now)
	b := mkSortIssue("ABC-2", "Bug", "To Do", "Highest", "x", nil, now, now)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "Low", "x", nil, now, now)

	got := sortBy(t, []model.Issue{a, b, c}, list.SortKey{Column: list.ColPrio, Desc: true})
	wantOrder := []string{"ABC-3", "ABC-1", "ABC-2"}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("sort by prio desc: pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_ByAssignee_caseInsensitive(t *testing.T) {
	now := time.Now()
	bob := &model.User{AccountID: "acc-bob", DisplayName: "bob"}
	alice := &model.User{AccountID: "acc-alice", DisplayName: "Alice"}
	carol := &model.User{AccountID: "acc-carol", DisplayName: "carol"}

	a := mkSortIssue("ABC-1", "Bug", "To Do", "", "x", bob, now, now)
	b := mkSortIssue("ABC-2", "Bug", "To Do", "", "x", alice, now, now)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "", "x", carol, now, now)
	d := mkSortIssue("ABC-4", "Bug", "To Do", "", "x", nil, now, now) // unassigned → ""

	got := sortBy(t, []model.Issue{a, b, c, d}, list.SortKey{Column: list.ColAssignee, Desc: false})
	// blank ("") sorts first; then Alice, bob, carol — case-insensitive.
	wantOrder := []string{"ABC-4", "ABC-2", "ABC-1", "ABC-3"}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("sort by assignee asc: pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_ByCreated_ascending(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := mkSortIssue("ABC-1", "Bug", "To Do", "", "x", nil, t0.Add(2*time.Hour), t0)
	b := mkSortIssue("ABC-2", "Bug", "To Do", "", "x", nil, t0, t0)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "", "x", nil, t0.Add(time.Hour), t0)

	got := sortBy(t, []model.Issue{a, b, c}, list.SortKey{Column: list.ColCreated, Desc: false})
	wantOrder := []string{"ABC-2", "ABC-3", "ABC-1"}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("sort by created asc: pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_ByCreated_equalCreatedPreservesUpstreamOrder(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Equal Created times → applySort is stable, so the upstream order
	// (memstore.List sorts by Updated desc) survives. ABC-B has the
	// later Updated, so it should remain first after sort-by-Created.
	a := mkSortIssue("ABC-A", "Bug", "To Do", "", "x", nil, t0, t0)
	b := mkSortIssue("ABC-B", "Bug", "To Do", "", "x", nil, t0, t0.Add(time.Hour))
	got := sortBy(t, []model.Issue{a, b}, list.SortKey{Column: list.ColCreated, Desc: false})
	if got[0].Key != "ABC-B" {
		t.Fatalf("stable sort broke (order should mirror Updated desc when Created ties): %v", keys(got))
	}
}

func TestSort_ByUpdated_ordersIssues(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := mkSortIssue("ABC-1", "Bug", "To Do", "", "x", nil, t0, t0.Add(2*time.Hour))
	b := mkSortIssue("ABC-2", "Bug", "To Do", "", "x", nil, t0, t0)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "", "x", nil, t0, t0.Add(time.Hour))

	got := sortBy(t, []model.Issue{a, b, c}, list.SortKey{Column: list.ColUpdated, Desc: true})
	wantOrder := []string{"ABC-1", "ABC-3", "ABC-2"}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_BySummary_caseInsensitive(t *testing.T) {
	now := time.Now()
	a := mkSortIssue("ABC-1", "Bug", "To Do", "", "banana", nil, now, now)
	b := mkSortIssue("ABC-2", "Bug", "To Do", "", "Apple", nil, now, now)
	c := mkSortIssue("ABC-3", "Bug", "To Do", "", "cherry", nil, now, now)

	got := sortBy(t, []model.Issue{a, b, c}, list.SortKey{Column: list.ColSummary, Desc: false})
	wantOrder := []string{"ABC-2", "ABC-1", "ABC-3"}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Fatalf("pos %d = %q, want %q (full=%v)", i, got[i].Key, k, keys(got))
		}
	}
}

func TestSort_MultiKey_secondaryActsAsTiebreaker(t *testing.T) {
	now := time.Now()
	bug1 := mkSortIssue("ABC-1", "Bug", "Done", "", "x", nil, now, now)
	bug2 := mkSortIssue("ABC-2", "Bug", "To Do", "", "x", nil, now, now)
	task1 := mkSortIssue("ABC-3", "Task", "Done", "", "x", nil, now, now)
	task2 := mkSortIssue("ABC-4", "Task", "To Do", "", "x", nil, now, now)

	// Sort by Type asc, then Status asc within each Type bucket.
	got := sortBy(t, []model.Issue{bug2, task1, bug1, task2},
		list.SortKey{Column: list.ColType, Desc: false},
		list.SortKey{Column: list.ColStatus, Desc: false},
	)
	want := []string{"ABC-1", "ABC-2", "ABC-3", "ABC-4"}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("multi-key sort: pos %d = %q, want %q", i, got[i].Key, k)
		}
	}
}

func keys(issues []model.Issue) []string {
	out := make([]string, len(issues))
	for i, iss := range issues {
		out[i] = iss.Key
	}
	return out
}

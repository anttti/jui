package model

import (
	"testing"
	"time"
)

func TestFilterEmpty(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		want bool
	}{
		{"zero value", Filter{}, true},
		{"search only", Filter{Search: "foo"}, false},
		{"types", Filter{Types: []string{"Bug"}}, false},
		{"statuses", Filter{Statuses: []string{"Open"}}, false},
		{"assignee Me", Filter{Assignee: AssigneeMe()}, false},
		{"assignee All is empty", Filter{Assignee: AssigneeAll()}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.f.Empty(); got != c.want {
				t.Fatalf("Empty()=%v, want %v", got, c.want)
			}
		})
	}
}

func TestAssigneeFilterKind(t *testing.T) {
	if AssigneeAll().Kind != AssigneeKindAll {
		t.Fatalf("AssigneeAll kind mismatch")
	}
	if AssigneeMe().Kind != AssigneeKindMe {
		t.Fatalf("AssigneeMe kind mismatch")
	}
	if AssigneeUnassigned().Kind != AssigneeKindUnassigned {
		t.Fatalf("AssigneeUnassigned kind mismatch")
	}
	a := AssigneeAccount("abc-123")
	if a.Kind != AssigneeKindAccount || a.AccountID != "abc-123" {
		t.Fatalf("AssigneeAccount built wrong: %+v", a)
	}
}

func TestIssueUnassigned(t *testing.T) {
	i := Issue{}
	if !i.Unassigned() {
		t.Fatalf("zero issue should be unassigned")
	}
	i.Assignee = &User{AccountID: "x"}
	if i.Unassigned() {
		t.Fatalf("issue with assignee should not be unassigned")
	}
}

func TestSyncStateNeedsInitial(t *testing.T) {
	var s SyncState
	if !s.NeedsInitialSync() {
		t.Fatalf("zero SyncState should need initial sync")
	}
	s.LastSyncUTC = time.Now()
	if s.NeedsInitialSync() {
		t.Fatalf("SyncState with watermark should not need initial")
	}
}

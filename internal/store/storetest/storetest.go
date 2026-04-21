// Package storetest provides a shared conformance suite that every
// store.Store implementation must pass. Behavioural parity between the
// in-memory and SQLite stores is enforced here, not by hand.
package storetest

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
)

// Factory constructs a fresh store for a single subtest.
type Factory func(t *testing.T) store.Store

// Run exercises the full conformance suite against the given factory.
func Run(t *testing.T, f Factory) {
	t.Helper()
	t.Run("Get missing returns nil", func(t *testing.T) { testGetMissing(t, f) })
	t.Run("Upsert then Get round-trips", func(t *testing.T) { testUpsertRoundTrip(t, f) })
	t.Run("Upsert is idempotent", func(t *testing.T) { testUpsertIdempotent(t, f) })
	t.Run("Get is case-insensitive", func(t *testing.T) { testGetCaseInsensitive(t, f) })
	t.Run("List empty", func(t *testing.T) { testListEmpty(t, f) })
	t.Run("List no filter returns all by Updated desc", func(t *testing.T) { testListAllOrder(t, f) })
	t.Run("List by type", func(t *testing.T) { testListByType(t, f) })
	t.Run("List by status", func(t *testing.T) { testListByStatus(t, f) })
	t.Run("List by assignee account", func(t *testing.T) { testListByAssigneeAccount(t, f) })
	t.Run("List unassigned", func(t *testing.T) { testListUnassigned(t, f) })
	t.Run("List combines filters AND", func(t *testing.T) { testListCombinedFilters(t, f) })
	t.Run("List search summary", func(t *testing.T) { testListSearchSummary(t, f) })
	t.Run("List search description", func(t *testing.T) { testListSearchDescription(t, f) })
	t.Run("List paging", func(t *testing.T) { testListPaging(t, f) })
	t.Run("Distinct types", func(t *testing.T) { testDistinctTypes(t, f) })
	t.Run("Distinct statuses", func(t *testing.T) { testDistinctStatuses(t, f) })
	t.Run("Assignees distinct", func(t *testing.T) { testAssigneesDistinct(t, f) })
	t.Run("Comments replace", func(t *testing.T) { testCommentsReplace(t, f) })
	t.Run("Comments ordered by created", func(t *testing.T) { testCommentsOrdered(t, f) })
	t.Run("Comments for missing issue", func(t *testing.T) { testCommentsMissingIssue(t, f) })
	t.Run("SyncState zero then round-trip", func(t *testing.T) { testSyncStateRoundTrip(t, f) })
}

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

var (
	t0   = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	alice = &model.User{AccountID: "acc-alice", DisplayName: "Alice", Email: "a@x"}
	bob   = &model.User{AccountID: "acc-bob", DisplayName: "Bob", Email: "b@x"}
)

func issue(key, summary, desc, typ, status string, assignee *model.User, updatedOffset time.Duration) model.Issue {
	return model.Issue{
		IssueRef:       model.IssueRef{Key: key, ID: key + "-id", ProjectKey: "ABC"},
		Summary:        summary,
		Description:    desc,
		Type:           typ,
		Status:         status,
		StatusCategory: categoryFor(status),
		Assignee:       assignee,
		Reporter:       alice,
		Labels:         []string{"auto"},
		Created:        t0,
		Updated:        t0.Add(updatedOffset),
		URL:            "https://x.atlassian.net/browse/" + key,
	}
}

func categoryFor(status string) string {
	switch status {
	case "Done":
		return "done"
	case "In Progress":
		return "indeterminate"
	default:
		return "todo"
	}
}

func upsert(t *testing.T, s store.Store, iss model.Issue) {
	t.Helper()
	if err := s.UpsertIssue(context.Background(), iss, []byte(`{}`)); err != nil {
		t.Fatalf("UpsertIssue(%s): %v", iss.Key, err)
	}
}

func keys(issues []model.Issue) []string {
	out := make([]string, len(issues))
	for i, iss := range issues {
		out[i] = iss.Key
	}
	return out
}

// -----------------------------------------------------------------------------
// Individual tests
// -----------------------------------------------------------------------------

func testGetMissing(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	got, err := s.Get(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing key, got %+v", got)
	}
}

func testUpsertRoundTrip(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	iss := issue("ABC-1", "Hello", "World", "Bug", "To Do", alice, 0)
	upsert(t, s, iss)
	got, err := s.Get(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatalf("expected issue, got nil")
	}
	if got.Key != "ABC-1" || got.Summary != "Hello" || got.Type != "Bug" {
		t.Fatalf("unexpected issue: %+v", got)
	}
}

func testUpsertIdempotent(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "v1", "d1", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-1", "v2", "d2", "Task", "In Progress", bob, time.Hour))
	got, err := s.Get(context.Background(), "ABC-1")
	if err != nil || got == nil {
		t.Fatalf("get after upsert: %v / %v", err, got)
	}
	if got.Summary != "v2" || got.Type != "Task" || got.Status != "In Progress" {
		t.Fatalf("second upsert did not replace: %+v", got)
	}
	list, total, err := s.List(context.Background(), model.Filter{}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected single row after idempotent upsert, got total=%d len=%d", total, len(list))
	}
}

func testGetCaseInsensitive(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "x", "", "Bug", "To Do", alice, 0))
	got, err := s.Get(context.Background(), "abc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.Key != "ABC-1" {
		t.Fatalf("expected case-insensitive hit, got %+v", got)
	}
}

func testListEmpty(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	list, total, err := s.List(context.Background(), model.Filter{}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 || len(list) != 0 {
		t.Fatalf("expected empty, got total=%d len=%d", total, len(list))
	}
}

func testListAllOrder(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "s1", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "s2", "", "Task", "Done", bob, 2*time.Hour))
	upsert(t, s, issue("ABC-3", "s3", "", "Bug", "In Progress", alice, time.Hour))
	list, total, err := s.List(context.Background(), model.Filter{}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3", total)
	}
	got := keys(list)
	want := []string{"ABC-2", "ABC-3", "ABC-1"}
	if !equalStrings(got, want) {
		t.Fatalf("order: got %v want %v", got, want)
	}
}

func testListByType(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Task", "To Do", alice, time.Hour))
	upsert(t, s, issue("ABC-3", "c", "", "Story", "To Do", alice, 2*time.Hour))
	list, total, err := s.List(context.Background(),
		model.Filter{Types: []string{"Bug", "Story"}}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("total=%d want 2", total)
	}
	if !equalStrings(keys(list), []string{"ABC-3", "ABC-1"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListByStatus(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "Done", alice, time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{Statuses: []string{"Done"}}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-2"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListByAssigneeAccount(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "To Do", bob, time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{Assignee: model.AssigneeAccount("acc-bob")}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-2"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListUnassigned(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "To Do", nil, time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{Assignee: model.AssigneeUnassigned()}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-2"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListCombinedFilters(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "alpha", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "beta", "", "Bug", "Done", alice, time.Hour))
	upsert(t, s, issue("ABC-3", "gamma", "", "Task", "To Do", alice, 2*time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{
			Types:    []string{"Bug"},
			Statuses: []string{"To Do"},
			Assignee: model.AssigneeAccount("acc-alice"),
		}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-1"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListSearchSummary(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "login flake", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "metrics", "", "Task", "To Do", alice, time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{Search: "flake"}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-1"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListSearchDescription(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "x", "quickfix description", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "y", "something else", "Bug", "To Do", alice, time.Hour))
	list, _, err := s.List(context.Background(),
		model.Filter{Search: "quickfix"}, model.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !equalStrings(keys(list), []string{"ABC-1"}) {
		t.Fatalf("got %v", keys(list))
	}
}

func testListPaging(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	for i := 0; i < 5; i++ {
		upsert(t, s, issue("ABC-"+itoa(i+1), "s", "", "Bug", "To Do", alice, time.Duration(i)*time.Hour))
	}
	// Newest first → ABC-5, ABC-4, ABC-3, ABC-2, ABC-1.
	list, total, err := s.List(context.Background(), model.Filter{}, model.Page{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 5 {
		t.Fatalf("total=%d want 5", total)
	}
	if !equalStrings(keys(list), []string{"ABC-4", "ABC-3"}) {
		t.Fatalf("paged keys: %v", keys(list))
	}
}

func testDistinctTypes(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "To Do", alice, time.Hour))
	upsert(t, s, issue("ABC-3", "c", "", "Task", "To Do", alice, 2*time.Hour))
	types, err := s.DistinctTypes(context.Background())
	if err != nil {
		t.Fatalf("DistinctTypes: %v", err)
	}
	if !equalSorted(types, []string{"Bug", "Task"}) {
		t.Fatalf("got %v", types)
	}
}

func testDistinctStatuses(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "Done", alice, time.Hour))
	statuses, err := s.DistinctStatuses(context.Background())
	if err != nil {
		t.Fatalf("DistinctStatuses: %v", err)
	}
	if !equalSorted(statuses, []string{"Done", "To Do"}) {
		t.Fatalf("got %v", statuses)
	}
}

func testAssigneesDistinct(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "a", "", "Bug", "To Do", alice, 0))
	upsert(t, s, issue("ABC-2", "b", "", "Bug", "To Do", alice, time.Hour))
	upsert(t, s, issue("ABC-3", "c", "", "Bug", "To Do", bob, 2*time.Hour))
	upsert(t, s, issue("ABC-4", "d", "", "Bug", "To Do", nil, 3*time.Hour))
	users, err := s.Assignees(context.Background())
	if err != nil {
		t.Fatalf("Assignees: %v", err)
	}
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.AccountID)
	}
	if !equalSorted(ids, []string{"acc-alice", "acc-bob"}) {
		t.Fatalf("got %v", ids)
	}
}

func testCommentsReplace(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "x", "", "Bug", "To Do", alice, 0))
	first := []model.Comment{
		{ID: "c1", IssueKey: "ABC-1", Author: alice, Body: "one", Created: t0},
		{ID: "c2", IssueKey: "ABC-1", Author: bob, Body: "two", Created: t0.Add(time.Hour)},
	}
	if err := s.ReplaceComments(context.Background(), "ABC-1", first); err != nil {
		t.Fatalf("ReplaceComments: %v", err)
	}
	got, err := s.Comments(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 comments, got %d", len(got))
	}
	second := []model.Comment{
		{ID: "c3", IssueKey: "ABC-1", Author: alice, Body: "only", Created: t0.Add(2 * time.Hour)},
	}
	if err := s.ReplaceComments(context.Background(), "ABC-1", second); err != nil {
		t.Fatalf("ReplaceComments #2: %v", err)
	}
	got, err = s.Comments(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 1 || got[0].ID != "c3" {
		t.Fatalf("expected only c3, got %+v", got)
	}
}

func testCommentsOrdered(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	upsert(t, s, issue("ABC-1", "x", "", "Bug", "To Do", alice, 0))
	cs := []model.Comment{
		{ID: "c2", IssueKey: "ABC-1", Author: alice, Body: "second", Created: t0.Add(time.Hour)},
		{ID: "c1", IssueKey: "ABC-1", Author: alice, Body: "first", Created: t0},
		{ID: "c3", IssueKey: "ABC-1", Author: alice, Body: "third", Created: t0.Add(2 * time.Hour)},
	}
	if err := s.ReplaceComments(context.Background(), "ABC-1", cs); err != nil {
		t.Fatalf("ReplaceComments: %v", err)
	}
	got, err := s.Comments(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	want := []string{"c1", "c2", "c3"}
	for i, c := range got {
		if c.ID != want[i] {
			t.Fatalf("comment order: got %s want %s at %d", c.ID, want[i], i)
		}
	}
}

func testCommentsMissingIssue(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	got, err := s.Comments(context.Background(), "NOPE-9")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no comments, got %d", len(got))
	}
}

func testSyncStateRoundTrip(t *testing.T, f Factory) {
	s := f(t)
	defer s.Close()
	got, err := s.SyncState(context.Background())
	if err != nil {
		t.Fatalf("SyncState: %v", err)
	}
	if !got.NeedsInitialSync() {
		t.Fatalf("expected initial state, got %+v", got)
	}
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	err = s.SetSyncState(context.Background(), model.SyncState{
		LastSyncUTC:    now,
		LastFullSyncAt: now,
		LastError:      "",
	})
	if err != nil {
		t.Fatalf("SetSyncState: %v", err)
	}
	got, err = s.SyncState(context.Background())
	if err != nil {
		t.Fatalf("SyncState: %v", err)
	}
	if !got.LastSyncUTC.Equal(now) {
		t.Fatalf("round-trip LastSyncUTC: got %v want %v", got.LastSyncUTC, now)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalSorted(a, b []string) bool {
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	return equalStrings(ac, bc)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	buf := []byte{}
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

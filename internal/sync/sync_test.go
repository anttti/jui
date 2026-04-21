package sync_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anttti/j/internal/jira"
	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store/memstore"
	syncpkg "github.com/anttti/j/internal/sync"
)

// -----------------------------------------------------------------------------
// fake jira
// -----------------------------------------------------------------------------

type fakeJira struct {
	// pagesByJQL maps a JQL string to an ordered list of SearchPages to
	// return for successive token-less→tokened calls.
	pagesByJQL map[string][]*jira.SearchPage
	issues     map[string]*jira.IssueEntry
	comments   map[string][]model.Comment

	searchErr error

	searchJQLs []string
	searchTokens []string
	commentCalls []string
}

func (f *fakeJira) Search(_ context.Context, jql, token string) (*jira.SearchPage, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	f.searchJQLs = append(f.searchJQLs, jql)
	f.searchTokens = append(f.searchTokens, token)
	pages, ok := f.pagesByJQL[jql]
	if !ok {
		// Empty page if JQL unknown — useful for "no updates" tests.
		return &jira.SearchPage{IsLast: true}, nil
	}
	// Determine which page to return based on how many we've already served
	// for this JQL.
	served := 0
	for i := range f.searchJQLs {
		if f.searchJQLs[i] == jql {
			served++
		}
	}
	idx := served - 1
	if idx >= len(pages) {
		return &jira.SearchPage{IsLast: true}, nil
	}
	return pages[idx], nil
}

func (f *fakeJira) Issue(_ context.Context, key string) (*jira.IssueEntry, error) {
	if e, ok := f.issues[key]; ok {
		return e, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeJira) Comments(_ context.Context, key string) ([]model.Comment, error) {
	f.commentCalls = append(f.commentCalls, key)
	return f.comments[key], nil
}

// -----------------------------------------------------------------------------
// fixtures
// -----------------------------------------------------------------------------

var (
	t0    = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	alice = &model.User{AccountID: "acc-alice", DisplayName: "Alice"}
	bob   = &model.User{AccountID: "acc-bob", DisplayName: "Bob"}
)

func entry(key, summary string, updated time.Time, assignee *model.User) jira.IssueEntry {
	iss := model.Issue{
		IssueRef: model.IssueRef{Key: key, ID: key + "-id", ProjectKey: "ABC"},
		Summary:  summary,
		Type:     "Bug",
		Status:   "To Do",
		Assignee: assignee,
		Created:  t0,
		Updated:  updated,
		URL:      "https://x.atlassian.net/browse/" + key,
	}
	raw, _ := json.Marshal(map[string]any{"key": key, "summary": summary})
	return jira.IssueEntry{Issue: iss, Raw: raw}
}

// -----------------------------------------------------------------------------
// tests
// -----------------------------------------------------------------------------

func TestRun_InitialSync_PopulatesStore(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	e1 := entry("ABC-1", "a", t0, alice)
	e2 := entry("ABC-2", "b", t0.Add(time.Hour), bob)
	now := t0
	expectedJQL := `(project = ABC) AND updated >= "2026-04-17 10:00" ORDER BY updated ASC`
	fj := &fakeJira{
		pagesByJQL: map[string][]*jira.SearchPage{
			expectedJQL: {{Entries: []jira.IssueEntry{e1, e2}, IsLast: true}},
		},
		comments: map[string][]model.Comment{
			"ABC-1": {{ID: "c1", IssueKey: "ABC-1", Body: "hi"}},
			"ABC-2": nil,
		},
	}
	eng := syncpkg.New(fj, s,
		syncpkg.WithClock(func() time.Time { return now }),
		syncpkg.WithInitialLookback(3*24*time.Hour),
	)
	if err := eng.Run(ctx, "project = ABC"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got1, _ := s.Get(ctx, "ABC-1")
	got2, _ := s.Get(ctx, "ABC-2")
	if got1 == nil || got2 == nil {
		t.Fatalf("issues not persisted: %v / %v", got1, got2)
	}
	cs, _ := s.Comments(ctx, "ABC-1")
	if len(cs) != 1 || cs[0].ID != "c1" {
		t.Fatalf("comments: %+v", cs)
	}

	// Watermark should equal the max(updated) across batch.
	state, _ := s.SyncState(ctx)
	if !state.LastSyncUTC.Equal(e2.Issue.Updated) {
		t.Fatalf("watermark=%v want %v", state.LastSyncUTC, e2.Issue.Updated)
	}
	if state.LastError != "" {
		t.Fatalf("unexpected LastError=%q", state.LastError)
	}

	// JQL should be the initial-lookback-derived one.
	if len(fj.searchJQLs) != 1 || fj.searchJQLs[0] != expectedJQL {
		t.Fatalf("jql=%v", fj.searchJQLs)
	}
}

func TestRun_Incremental_UsesWatermarkInJQL(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	wm := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	_ = s.SetSyncState(ctx, model.SyncState{LastSyncUTC: wm})

	fj := &fakeJira{pagesByJQL: map[string][]*jira.SearchPage{}}
	eng := syncpkg.New(fj, s,
		syncpkg.WithClock(func() time.Time { return wm.Add(time.Hour) }),
	)
	_ = eng.Run(ctx, "project = ABC")
	if len(fj.searchJQLs) != 1 {
		t.Fatalf("calls: %d", len(fj.searchJQLs))
	}
	jql := fj.searchJQLs[0]
	if !strings.Contains(jql, `updated >= "2026-04-20 09:00"`) {
		t.Fatalf("jql missing watermark: %s", jql)
	}
	if !strings.Contains(jql, "project = ABC") {
		t.Fatalf("jql missing user jql: %s", jql)
	}
	if !strings.Contains(jql, "ORDER BY updated ASC") {
		t.Fatalf("jql missing order: %s", jql)
	}
}

func TestRun_ErrorPreservesWatermark(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	wm := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	_ = s.SetSyncState(ctx, model.SyncState{LastSyncUTC: wm})

	fj := &fakeJira{searchErr: errors.New("boom")}
	eng := syncpkg.New(fj, s,
		syncpkg.WithClock(func() time.Time { return wm.Add(time.Hour) }),
	)
	err := eng.Run(ctx, "project = ABC")
	if err == nil {
		t.Fatalf("expected error")
	}
	state, _ := s.SyncState(ctx)
	if !state.LastSyncUTC.Equal(wm) {
		t.Fatalf("watermark changed on error: %v want %v", state.LastSyncUTC, wm)
	}
	if state.LastError == "" {
		t.Fatalf("LastError not set")
	}
}

func TestRun_CommentsReplacedNotDuplicated(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	e1 := entry("ABC-1", "a", t0, alice)
	fj := &fakeJira{
		pagesByJQL: map[string][]*jira.SearchPage{},
		comments:   map[string][]model.Comment{"ABC-1": {{ID: "c1", IssueKey: "ABC-1", Body: "v1"}}},
	}
	// Any search returns our single issue. Easier to just map a sentinel
	// JQL used twice.
	fj.pagesByJQL[""] = nil // force fallback to empty

	// Manually stub Search to always return the same page.
	fj2 := &alwaysSameSearchFake{fake: fj, page: &jira.SearchPage{Entries: []jira.IssueEntry{e1}, IsLast: true}}

	eng := syncpkg.New(fj2, s,
		syncpkg.WithClock(func() time.Time { return t0.Add(time.Hour) }),
	)
	if err := eng.Run(ctx, "project = ABC"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Rotate comment content for second run.
	fj.comments["ABC-1"] = []model.Comment{{ID: "c2", IssueKey: "ABC-1", Body: "v2"}}
	if err := eng.Run(ctx, "project = ABC"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	cs, _ := s.Comments(ctx, "ABC-1")
	if len(cs) != 1 || cs[0].ID != "c2" {
		t.Fatalf("expected exactly the new comment, got %+v", cs)
	}
}

func TestRun_PaginatesThroughMultiplePages(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	e1 := entry("ABC-1", "a", t0, alice)
	e2 := entry("ABC-2", "b", t0.Add(time.Hour), bob)
	page1 := &jira.SearchPage{Entries: []jira.IssueEntry{e1}, NextPageToken: "tk2", IsLast: false}
	page2 := &jira.SearchPage{Entries: []jira.IssueEntry{e2}, IsLast: true}

	fj := &multiPageFake{pages: []*jira.SearchPage{page1, page2}, comments: map[string][]model.Comment{}}
	eng := syncpkg.New(fj, s,
		syncpkg.WithClock(func() time.Time { return t0.Add(2 * time.Hour) }),
	)
	if err := eng.Run(ctx, "project = ABC"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fj.calls != 2 {
		t.Fatalf("expected 2 search calls, got %d", fj.calls)
	}
	if fj.tokens[1] != "tk2" {
		t.Fatalf("expected second call with token tk2, got %q", fj.tokens[1])
	}
	g1, _ := s.Get(ctx, "ABC-1")
	g2, _ := s.Get(ctx, "ABC-2")
	if g1 == nil || g2 == nil {
		t.Fatalf("both pages should populate: %v / %v", g1, g2)
	}
}

func TestFetchOne_UpsertsIssueAndComments(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	e := entry("ABC-9", "solo", t0, alice)
	fj := &fakeJira{
		issues: map[string]*jira.IssueEntry{"ABC-9": &e},
		comments: map[string][]model.Comment{
			"ABC-9": {{ID: "cx", IssueKey: "ABC-9", Body: "hi"}},
		},
	}
	eng := syncpkg.New(fj, s)
	if err := eng.FetchOne(ctx, "ABC-9"); err != nil {
		t.Fatalf("FetchOne: %v", err)
	}
	got, _ := s.Get(ctx, "ABC-9")
	if got == nil || got.Summary != "solo" {
		t.Fatalf("issue not fetched: %+v", got)
	}
	cs, _ := s.Comments(ctx, "ABC-9")
	if len(cs) != 1 || cs[0].ID != "cx" {
		t.Fatalf("comments: %+v", cs)
	}
}

// -----------------------------------------------------------------------------
// helper fakes
// -----------------------------------------------------------------------------

type alwaysSameSearchFake struct {
	*fakeJira
	fake *fakeJira
	page *jira.SearchPage
}

func (a *alwaysSameSearchFake) Search(_ context.Context, jql, token string) (*jira.SearchPage, error) {
	return a.page, nil
}
func (a *alwaysSameSearchFake) Comments(ctx context.Context, key string) ([]model.Comment, error) {
	return a.fake.Comments(ctx, key)
}
func (a *alwaysSameSearchFake) Issue(ctx context.Context, key string) (*jira.IssueEntry, error) {
	return a.fake.Issue(ctx, key)
}

type multiPageFake struct {
	pages    []*jira.SearchPage
	comments map[string][]model.Comment
	calls    int
	tokens   []string
}

func (m *multiPageFake) Search(_ context.Context, jql, token string) (*jira.SearchPage, error) {
	m.tokens = append(m.tokens, token)
	if m.calls >= len(m.pages) {
		return &jira.SearchPage{IsLast: true}, nil
	}
	p := m.pages[m.calls]
	m.calls++
	return p, nil
}
func (m *multiPageFake) Issue(_ context.Context, _ string) (*jira.IssueEntry, error) {
	return nil, errors.New("not implemented")
}
func (m *multiPageFake) Comments(_ context.Context, key string) ([]model.Comment, error) {
	return m.comments[key], nil
}

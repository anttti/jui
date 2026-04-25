package jiramock_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anttti/j/internal/jira"
	"github.com/anttti/j/internal/jiramock"
)

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func newClient(t *testing.T, srv *jiramock.Server) *jira.Client {
	t.Helper()
	c, err := jira.New(jira.Config{
		BaseURL: srv.URL(),
		Email:   "me@example.com",
		Token:   "tok",
	}, jira.WithSleeper(func(time.Duration) {}))
	if err != nil {
		t.Fatalf("jira.New: %v", err)
	}
	return c
}

var t0 = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

// -----------------------------------------------------------------------------
// schema fidelity: raw HTTP shape matches Atlassian's docs
// -----------------------------------------------------------------------------

func TestServer_IssueResponseShapeMatchesSchema(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.AddIssue(jiramock.Issue{
		Key:            "ABC-1",
		Summary:        "Boom",
		Description:    "It exploded.",
		Type:           "Bug",
		Status:         "In Progress",
		StatusCategory: "indeterminate",
		Priority:       "High",
		Labels:         []string{"flaky"},
		Created:        t0,
		Updated:        t0.Add(time.Hour),
		Assignee:       &jiramock.User{AccountID: "acc-a", DisplayName: "Alice"},
	})

	resp, err := http.Get(srv.URL() + "/rest/api/3/issue/ABC-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, body)
	}
	// Top-level keys per IssueBean.
	for _, k := range []string{"id", "key", "self", "fields"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing top-level key %q in %s", k, body)
		}
	}
	fields, _ := got["fields"].(map[string]any)
	for _, k := range []string{"summary", "description", "issuetype", "status", "project", "labels", "created", "updated"} {
		if _, ok := fields[k]; !ok {
			t.Errorf("missing fields.%s in %s", k, body)
		}
	}
	// Description must be an ADF doc.
	desc, _ := fields["description"].(map[string]any)
	if desc["type"] != "doc" {
		t.Errorf("description not an ADF doc: %v", desc)
	}
	// Status must carry a statusCategory.key.
	status, _ := fields["status"].(map[string]any)
	cat, _ := status["statusCategory"].(map[string]any)
	if cat["key"] != "indeterminate" {
		t.Errorf("status.statusCategory.key=%v want indeterminate", cat["key"])
	}
}

// -----------------------------------------------------------------------------
// end-to-end: real *jira.Client decodes mock JSON
// -----------------------------------------------------------------------------

func TestClient_Myself_AgainstMock(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.SetMyself(jiramock.User{AccountID: "acc-me", DisplayName: "Me", EmailAddress: "me@example.com"})

	c := newClient(t, srv)
	u, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if u.AccountID != "acc-me" || u.DisplayName != "Me" || u.Email != "me@example.com" {
		t.Fatalf("got %+v", u)
	}
}

func TestClient_Myself_NotConfiguredIs401(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	c := newClient(t, srv)
	if _, err := c.Myself(context.Background()); err == nil {
		t.Fatalf("expected error when no user configured")
	}
}

func TestClient_Issue_RoundTrip(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.AddIssue(jiramock.Issue{
		Key:            "ABC-1",
		Summary:        "Fix login",
		Description:    "Login is flaky.",
		Type:           "Bug",
		Status:         "In Progress",
		StatusCategory: "indeterminate",
		Priority:       "High",
		Assignee:       &jiramock.User{AccountID: "acc-a", DisplayName: "Alice", EmailAddress: "a@x"},
		Reporter:       &jiramock.User{AccountID: "acc-b", DisplayName: "Bob"},
		Labels:         []string{"flaky", "auth"},
		DueDate:        time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Created:        t0,
		Updated:        t0.Add(90 * time.Minute),
	})
	c := newClient(t, srv)
	entry, err := c.Issue(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	iss := entry.Issue
	if iss.Key != "ABC-1" || iss.ProjectKey != "ABC" {
		t.Fatalf("ids: %+v", iss.IssueRef)
	}
	if iss.Type != "Bug" || iss.Status != "In Progress" || iss.StatusCategory != "indeterminate" {
		t.Fatalf("type/status: %+v", iss)
	}
	if iss.Priority != "High" {
		t.Fatalf("priority=%q", iss.Priority)
	}
	if iss.Assignee == nil || iss.Assignee.DisplayName != "Alice" {
		t.Fatalf("assignee: %+v", iss.Assignee)
	}
	if iss.Reporter == nil || iss.Reporter.AccountID != "acc-b" {
		t.Fatalf("reporter: %+v", iss.Reporter)
	}
	if len(iss.Labels) != 2 || iss.Labels[1] != "auth" {
		t.Fatalf("labels: %+v", iss.Labels)
	}
	if iss.DueDate == nil || iss.DueDate.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("due: %+v", iss.DueDate)
	}
	if !iss.Created.Equal(t0) || !iss.Updated.Equal(t0.Add(90*time.Minute)) {
		t.Fatalf("times: %v / %v", iss.Created, iss.Updated)
	}
	if !strings.Contains(iss.Description, "Login is flaky") {
		t.Fatalf("desc not rendered: %q", iss.Description)
	}
	if !strings.HasSuffix(iss.URL, "/browse/ABC-1") {
		t.Fatalf("url: %q", iss.URL)
	}
}

func TestClient_Issue_MissingIs404(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	c := newClient(t, srv)
	_, err := c.Issue(context.Background(), "NOPE-1")
	if err == nil || !jira.IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// search + pagination
// -----------------------------------------------------------------------------

func TestClient_Search_PaginatesAndFiltersByProject(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.SetPageSize(2)
	for i := 1; i <= 5; i++ {
		srv.AddIssue(jiramock.Issue{
			Key:        fmtKey("ABC", i),
			Summary:    "issue",
			Updated:    t0.Add(time.Duration(i) * time.Minute),
			Created:    t0,
			Type:       "Task",
			Status:     "To Do",
			ProjectKey: "ABC",
		})
	}
	// One issue in another project — must be filtered out.
	srv.AddIssue(jiramock.Issue{Key: "OTHER-1", ProjectKey: "OTHER", Updated: t0, Created: t0})

	c := newClient(t, srv)
	var keys []string
	tok := ""
	for i := 0; i < 10; i++ {
		page, err := c.Search(context.Background(), "project = ABC ORDER BY updated ASC", tok)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, e := range page.Entries {
			keys = append(keys, e.Issue.Key)
		}
		if page.IsLast {
			break
		}
		tok = page.NextPageToken
	}
	want := []string{"ABC-1", "ABC-2", "ABC-3", "ABC-4", "ABC-5"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", keys, want)
	}

	// Verify we paginated rather than slurping everything in one page.
	calls := 0
	for _, r := range srv.Requests() {
		if r.Path == "/rest/api/3/search/jql" {
			calls++
		}
	}
	if calls < 3 {
		t.Fatalf("expected >=3 search calls (page size 2 over 5 issues), got %d", calls)
	}
}

func TestClient_Search_RespectsUpdatedSinceClause(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.AddIssue(jiramock.Issue{Key: "ABC-1", ProjectKey: "ABC", Updated: t0})
	srv.AddIssue(jiramock.Issue{Key: "ABC-2", ProjectKey: "ABC", Updated: t0.Add(2 * time.Hour)})

	c := newClient(t, srv)
	cutoff := t0.Add(time.Hour).Format("2006-01-02 15:04")
	jql := `(project = ABC) AND updated >= "` + cutoff + `" ORDER BY updated ASC`
	page, err := c.Search(context.Background(), jql, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Issue.Key != "ABC-2" {
		t.Fatalf("expected only ABC-2, got %+v", keysOf(page))
	}
}

// -----------------------------------------------------------------------------
// comments
// -----------------------------------------------------------------------------

func TestClient_Comments_RoundTrip(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.AddIssue(jiramock.Issue{Key: "ABC-1", ProjectKey: "ABC", Updated: t0})
	srv.AddComment("ABC-1", jiramock.Comment{
		ID:      "c1",
		Author:  &jiramock.User{AccountID: "acc-a", DisplayName: "Alice"},
		Body:    "Looks good",
		Created: t0,
		Updated: t0,
	})
	srv.AddComment("ABC-1", jiramock.Comment{
		ID:      "c2",
		Author:  &jiramock.User{DisplayName: "Bob"},
		Body:    "Done",
		Created: t0.Add(time.Minute),
		Updated: t0.Add(time.Minute),
	})

	c := newClient(t, srv)
	cs, err := c.Comments(context.Background(), "ABC-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(cs) != 2 || cs[0].ID != "c1" || cs[1].Body != "Done" {
		t.Fatalf("comments: %+v", cs)
	}
	if cs[0].Author == nil || cs[0].Author.DisplayName != "Alice" {
		t.Fatalf("author: %+v", cs[0].Author)
	}
}

// -----------------------------------------------------------------------------
// fault injection
// -----------------------------------------------------------------------------

func TestServer_FaultInjection_429ThenSuccess(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.SetMyself(jiramock.User{AccountID: "acc-me", DisplayName: "Me"})
	srv.InjectFault(jiramock.Fault{
		Path: "/rest/api/3/myself", Status: 429, RetryAfter: "0", Times: 2,
	})
	c := newClient(t, srv)
	if _, err := c.Myself(context.Background()); err != nil {
		t.Fatalf("Myself after retries: %v", err)
	}
	calls := 0
	for _, r := range srv.Requests() {
		if r.Path == "/rest/api/3/myself" {
			calls++
		}
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 fault + 1 success), got %d", calls)
	}
}

func TestServer_Reset_ClearsState(t *testing.T) {
	srv := jiramock.New()
	defer srv.Close()
	srv.AddIssue(jiramock.Issue{Key: "ABC-1", Updated: t0})
	srv.SetMyself(jiramock.User{AccountID: "acc"})
	srv.Reset()

	c := newClient(t, srv)
	if _, err := c.Issue(context.Background(), "ABC-1"); !jira.IsNotFound(err) {
		t.Fatalf("expected 404 after reset, got %v", err)
	}
	if _, err := c.Myself(context.Background()); err == nil {
		t.Fatalf("expected 401 after reset")
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func fmtKey(proj string, n int) string {
	return proj + "-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func keysOf(p *jira.SearchPage) []string {
	out := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		out = append(out, e.Issue.Key)
	}
	return out
}

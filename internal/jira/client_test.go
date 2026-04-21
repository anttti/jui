package jira_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anttti/j/internal/jira"
)

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

func newTestServer(t *testing.T, routes []route) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for _, r := range routes {
		r := r
		mux.HandleFunc(r.path, func(w http.ResponseWriter, req *http.Request) {
			if r.method != "" && req.Method != r.method {
				t.Errorf("%s %s: got method %s, want %s", r.method, r.path, req.Method, r.method)
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			r.handler(w, req)
		})
	}
	return httptest.NewServer(mux)
}

func newClient(t *testing.T, srv *httptest.Server, opts ...jira.Option) *jira.Client {
	t.Helper()
	c, err := jira.New(jira.Config{
		BaseURL: srv.URL,
		Email:   "me@example.com",
		Token:   "tok",
	}, opts...)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c
}

// -----------------------------------------------------------------------------
// Myself
// -----------------------------------------------------------------------------

func TestClient_Myself_sendsBasicAuthAndParses(t *testing.T) {
	srv := newTestServer(t, []route{{
		method: "GET",
		path:   "/rest/api/3/myself",
		handler: func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@example.com:tok"))
			if auth != want {
				t.Errorf("auth header: got %q want %q", auth, want)
			}
			if r.Header.Get("Accept") != "application/json" {
				t.Errorf("missing Accept header")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"accountId":"acc-me",
				"emailAddress":"me@example.com",
				"displayName":"Me"
			}`))
		},
	}})
	defer srv.Close()

	c := newClient(t, srv)
	u, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if u == nil || u.AccountID != "acc-me" || u.DisplayName != "Me" || u.Email != "me@example.com" {
		t.Fatalf("got %+v", u)
	}
}

func TestClient_Myself_401isError(t *testing.T) {
	srv := newTestServer(t, []route{{
		path: "/rest/api/3/myself",
		handler: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"errorMessages":["unauth"]}`, http.StatusUnauthorized)
		},
	}})
	defer srv.Close()
	c := newClient(t, srv)
	if _, err := c.Myself(context.Background()); err == nil {
		t.Fatalf("expected error on 401")
	}
}

// -----------------------------------------------------------------------------
// Issue (with ADF → markdown)
// -----------------------------------------------------------------------------

const issueJSON = `{
  "id": "10001",
  "key": "ABC-123",
  "fields": {
    "summary": "Fix flaky login test",
    "description": {
      "type": "doc",
      "version": 1,
      "content": [
        {"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Problem"}]},
        {"type":"paragraph","content":[{"type":"text","text":"Login "},{"type":"text","text":"flakes","marks":[{"type":"strong"}]},{"type":"text","text":" sometimes."}]},
        {"type":"bulletList","content":[
          {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Reproduce"}]}]},
          {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Fix"}]}]}
        ]}
      ]
    },
    "issuetype": {"name": "Bug"},
    "status": {"name": "In Progress", "statusCategory": {"key": "indeterminate"}},
    "priority": {"name": "High"},
    "project": {"key": "ABC"},
    "assignee": {"accountId":"acc-alice","displayName":"Alice","emailAddress":"a@x"},
    "reporter": {"accountId":"acc-bob","displayName":"Bob","emailAddress":"b@x"},
    "labels": ["flaky","auth"],
    "duedate": "2026-05-01",
    "created": "2026-04-20T10:00:00.000+0000",
    "updated": "2026-04-20T11:30:00.000+0000"
  }
}`

func TestClient_Issue_parsesFieldsAndADF(t *testing.T) {
	srv := newTestServer(t, []route{{
		method: "GET",
		path:   "/rest/api/3/issue/ABC-123",
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(issueJSON))
		},
	}})
	defer srv.Close()
	c := newClient(t, srv)

	entry, err := c.Issue(context.Background(), "ABC-123")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	iss := entry.Issue
	if iss.Key != "ABC-123" || iss.ID != "10001" || iss.ProjectKey != "ABC" {
		t.Fatalf("ids: %+v", iss.IssueRef)
	}
	if iss.Summary != "Fix flaky login test" {
		t.Fatalf("summary: %q", iss.Summary)
	}
	if iss.Type != "Bug" || iss.Status != "In Progress" || iss.StatusCategory != "indeterminate" {
		t.Fatalf("type/status: %+v", iss)
	}
	if iss.Priority != "High" {
		t.Fatalf("priority: %q", iss.Priority)
	}
	if iss.Assignee == nil || iss.Assignee.AccountID != "acc-alice" || iss.Assignee.DisplayName != "Alice" {
		t.Fatalf("assignee: %+v", iss.Assignee)
	}
	if iss.Reporter == nil || iss.Reporter.AccountID != "acc-bob" {
		t.Fatalf("reporter: %+v", iss.Reporter)
	}
	if len(iss.Labels) != 2 || iss.Labels[0] != "flaky" {
		t.Fatalf("labels: %+v", iss.Labels)
	}
	if iss.DueDate == nil || iss.DueDate.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("due: %+v", iss.DueDate)
	}
	if !iss.Created.Equal(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("created: %v", iss.Created)
	}
	if !iss.Updated.Equal(time.Date(2026, 4, 20, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("updated: %v", iss.Updated)
	}
	if !strings.HasSuffix(iss.URL, "/browse/ABC-123") {
		t.Fatalf("url: %q", iss.URL)
	}
	// ADF must render to markdown.
	d := iss.Description
	if !strings.Contains(d, "# Problem") {
		t.Fatalf("heading missing in desc: %q", d)
	}
	if !strings.Contains(d, "**flakes**") {
		t.Fatalf("strong mark missing: %q", d)
	}
	if !strings.Contains(d, "- Reproduce") || !strings.Contains(d, "- Fix") {
		t.Fatalf("bullet items missing: %q", d)
	}
	if len(entry.Raw) == 0 {
		t.Fatalf("raw JSON not preserved")
	}
}

func TestClient_Issue_404returnsNotFound(t *testing.T) {
	srv := newTestServer(t, []route{{
		path: "/rest/api/3/issue/NOPE-1",
		handler: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"errorMessages":["does not exist"]}`, http.StatusNotFound)
		},
	}})
	defer srv.Close()
	c := newClient(t, srv)
	_, err := c.Issue(context.Background(), "NOPE-1")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !jira.IsNotFound(err) {
		t.Fatalf("expected IsNotFound(err)=true, err=%v", err)
	}
}

// -----------------------------------------------------------------------------
// Search (paginated)
// -----------------------------------------------------------------------------

func TestClient_Search_paginates(t *testing.T) {
	// Two pages. First returns nextPageToken, second is last.
	page1 := `{
      "issues": [` + issueJSON + `],
      "nextPageToken": "tok2",
      "isLast": false
    }`
	page2 := `{
      "issues": [],
      "isLast": true
    }`
	var calls int
	srv := newTestServer(t, []route{{
		method: "GET",
		path:   "/rest/api/3/search/jql",
		handler: func(w http.ResponseWriter, r *http.Request) {
			calls++
			jql := r.URL.Query().Get("jql")
			if jql != `project = ABC` {
				t.Errorf("jql=%q", jql)
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("nextPageToken") {
			case "":
				w.Write([]byte(page1))
			case "tok2":
				w.Write([]byte(page2))
			default:
				t.Fatalf("unexpected nextPageToken=%s", r.URL.Query().Get("nextPageToken"))
			}
		},
	}})
	defer srv.Close()
	c := newClient(t, srv)

	p1, err := c.Search(context.Background(), "project = ABC", "")
	if err != nil {
		t.Fatalf("search p1: %v", err)
	}
	if len(p1.Entries) != 1 || p1.Entries[0].Issue.Key != "ABC-123" {
		t.Fatalf("p1 issues: %+v", p1.Entries)
	}
	if p1.NextPageToken != "tok2" || p1.IsLast {
		t.Fatalf("p1 paging: %+v", p1)
	}

	p2, err := c.Search(context.Background(), "project = ABC", p1.NextPageToken)
	if err != nil {
		t.Fatalf("search p2: %v", err)
	}
	if !p2.IsLast || p2.NextPageToken != "" {
		t.Fatalf("p2 paging: %+v", p2)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

// -----------------------------------------------------------------------------
// Comments
// -----------------------------------------------------------------------------

func TestClient_Comments_parses(t *testing.T) {
	body := `{
      "comments": [
        {
          "id":"c1",
          "author":{"accountId":"acc-alice","displayName":"Alice","emailAddress":"a@x"},
          "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good"}]}]},
          "created":"2026-04-20T12:00:00.000+0000",
          "updated":"2026-04-20T12:00:00.000+0000"
        },
        {
          "id":"c2",
          "author":{"accountId":"acc-bob","displayName":"Bob"},
          "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Done"}]}]},
          "created":"2026-04-20T13:00:00.000+0000",
          "updated":"2026-04-20T13:05:00.000+0000"
        }
      ],
      "total": 2,
      "startAt": 0,
      "maxResults": 100
    }`
	srv := newTestServer(t, []route{{
		method: "GET",
		path:   "/rest/api/3/issue/ABC-123/comment",
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		},
	}})
	defer srv.Close()

	c := newClient(t, srv)
	cs, err := c.Comments(context.Background(), "ABC-123")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("len=%d", len(cs))
	}
	if cs[0].ID != "c1" || cs[0].IssueKey != "ABC-123" || cs[0].Body != "Looks good" {
		t.Fatalf("c1: %+v", cs[0])
	}
	if cs[0].Author == nil || cs[0].Author.DisplayName != "Alice" {
		t.Fatalf("c1 author: %+v", cs[0].Author)
	}
	if cs[1].Body != "Done" {
		t.Fatalf("c2 body: %+v", cs[1])
	}
	if !cs[0].Created.Equal(time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("c1 created: %v", cs[0].Created)
	}
}

// -----------------------------------------------------------------------------
// Retry / backoff
// -----------------------------------------------------------------------------

func TestClient_Retry_on429RespectsRetryAfter(t *testing.T) {
	var calls int
	srv := newTestServer(t, []route{{
		path: "/rest/api/3/myself",
		handler: func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls < 3 {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"accountId":"acc-me","displayName":"Me","emailAddress":"me@example.com"}`))
		},
	}})
	defer srv.Close()

	var slept []time.Duration
	c := newClient(t, srv, jira.WithSleeper(func(d time.Duration) { slept = append(slept, d) }))
	if _, err := c.Myself(context.Background()); err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
	if len(slept) != 2 || slept[0] < time.Second {
		t.Fatalf("slept=%v want at least Retry-After", slept)
	}
}

func TestClient_Retry_on5xxBacksOff(t *testing.T) {
	var calls int
	srv := newTestServer(t, []route{{
		path: "/rest/api/3/myself",
		handler: func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls < 3 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"accountId":"acc-me","displayName":"Me","emailAddress":"me@example.com"}`))
		},
	}})
	defer srv.Close()

	var slept []time.Duration
	c := newClient(t, srv, jira.WithSleeper(func(d time.Duration) { slept = append(slept, d) }))
	if _, err := c.Myself(context.Background()); err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
	if len(slept) != 2 || slept[0] <= 0 || slept[1] <= slept[0] {
		t.Fatalf("backoff not exponential: %v", slept)
	}
}

func TestClient_Retry_giveUpAfterMax(t *testing.T) {
	srv := newTestServer(t, []route{{
		path: "/rest/api/3/myself",
		handler: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
	}})
	defer srv.Close()
	c := newClient(t, srv,
		jira.WithSleeper(func(time.Duration) {}),
		jira.WithMaxRetries(2),
	)
	_, err := c.Myself(context.Background())
	if err == nil {
		t.Fatalf("expected error after retries")
	}
}

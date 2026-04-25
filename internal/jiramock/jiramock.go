// Package jiramock is an in-memory mock of the Atlassian Jira Cloud
// REST API v3. Response shapes match Atlassian's published OpenAPI
// schema (see https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json),
// so a *jira.Client wired to a mock server.URL exercises real request
// building, retry behaviour, and JSON decoding.
//
// Endpoints served:
//
//   - GET /rest/api/3/myself              -> User
//   - GET /rest/api/3/issue/{key}         -> IssueBean
//   - GET /rest/api/3/issue/{key}/comment -> PageOfComments
//   - GET /rest/api/3/search/jql          -> SearchAndReconcileResults
//
// Typical use:
//
//	srv := jiramock.New()
//	defer srv.Close()
//	srv.SetMyself(jiramock.User{AccountID: "acc-me", DisplayName: "Me"})
//	srv.AddIssue(jiramock.Issue{Key: "ABC-1", Summary: "Boom", ...})
//
//	c, _ := jira.New(jira.Config{BaseURL: srv.URL(), Email: "x", Token: "y"})
//	got, _ := c.Issue(context.Background(), "ABC-1")
package jiramock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Public types
// -----------------------------------------------------------------------------

// User is a test-friendly user, expanded into the full schema on the wire.
type User struct {
	AccountID    string
	DisplayName  string
	EmailAddress string
	AccountType  string // "atlassian", "app", "customer"; defaults to "atlassian"
}

// Issue is a test-friendly issue. Description is plain text; it's wrapped
// in a single-paragraph ADF doc unless DescriptionADF is set.
type Issue struct {
	ID             string
	Key            string
	ProjectKey     string
	Summary        string
	Description    string
	DescriptionADF json.RawMessage
	Type           string
	Status         string
	StatusCategory string // "new", "indeterminate", "done"
	Priority       string
	Assignee       *User
	Reporter       *User
	Labels         []string
	DueDate        time.Time // zero = unset
	Created        time.Time
	Updated        time.Time
}

// Comment is a test-friendly comment.
type Comment struct {
	ID      string
	Author  *User
	Body    string
	BodyADF json.RawMessage
	Created time.Time
	Updated time.Time
}

// Fault forces non-2xx responses for matching requests. Status, RetryAfter,
// and Body define the failure; Times bounds how often it fires (0 = once).
// Method "" matches any. Path is matched against the request path with
// path.Match — use "*" wildcards.
type Fault struct {
	Method     string
	Path       string
	Status     int
	Body       string
	RetryAfter string
	Times      int
}

// Request is a recorded HTTP call.
type Request struct {
	Method string
	Path   string
	Query  url.Values
}

// -----------------------------------------------------------------------------
// Server
// -----------------------------------------------------------------------------

// Server is the in-memory mock. Construct with New, dispose with Close.
type Server struct {
	httpsrv *httptest.Server

	mu       sync.Mutex
	me       *User
	issues   map[string]*Issue
	comments map[string][]*Comment
	pageSize int
	faults   []Fault
	requests []Request
}

// New starts a mock server with no data. Call Close when done.
func New() *Server {
	s := &Server{
		issues:   map[string]*Issue{},
		comments: map[string][]*Comment{},
		pageSize: 100,
	}
	s.httpsrv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL returns the server's base URL, suitable for jira.Config.BaseURL.
func (s *Server) URL() string { return s.httpsrv.URL }

// Close shuts the server down.
func (s *Server) Close() { s.httpsrv.Close() }

// SetPageSize controls how many issues a /search/jql page returns. Default 100.
func (s *Server) SetPageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > 0 {
		s.pageSize = n
	}
}

// SetMyself sets the user returned by GET /rest/api/3/myself.
func (s *Server) SetMyself(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uu := u
	s.me = &uu
}

// AddIssue registers (or replaces) an issue. Key is required.
func (s *Server) AddIssue(iss Issue) {
	if iss.Key == "" {
		panic("jiramock: AddIssue requires Key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := iss
	if cp.ID == "" {
		cp.ID = "10" + cp.Key
	}
	if cp.ProjectKey == "" {
		if i := strings.IndexByte(cp.Key, '-'); i > 0 {
			cp.ProjectKey = cp.Key[:i]
		}
	}
	s.issues[cp.Key] = &cp
}

// AddComment appends a comment to an issue.
func (s *Server) AddComment(issueKey string, c Comment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := c
	s.comments[issueKey] = append(s.comments[issueKey], &cp)
}

// Reset clears all data, faults, and recorded requests.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.me = nil
	s.issues = map[string]*Issue{}
	s.comments = map[string][]*Comment{}
	s.faults = nil
	s.requests = nil
}

// InjectFault enqueues a fault. Faults fire in the order they were added,
// each consuming one match before the next is considered.
func (s *Server) InjectFault(f Fault) {
	if f.Times <= 0 {
		f.Times = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = append(s.faults, f)
}

// Requests returns a copy of the recorded request log.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// -----------------------------------------------------------------------------
// HTTP handler
// -----------------------------------------------------------------------------

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: r.Method, Path: r.URL.Path, Query: cloneValues(r.URL.Query()),
	})
	if f := s.matchFault(r); f != nil {
		if f.RetryAfter != "" {
			w.Header().Set("Retry-After", f.RetryAfter)
		}
		body := f.Body
		if body == "" {
			body = `{"errorMessages":["injected fault"]}`
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.Status)
		_, _ = w.Write([]byte(body))
		return
	}
	s.mu.Unlock()

	switch {
	case r.URL.Path == "/rest/api/3/myself":
		s.handleMyself(w, r)
	case r.URL.Path == "/rest/api/3/search/jql":
		s.handleSearch(w, r)
	case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
		rest := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		// rest is "KEY" or "KEY/comment"
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			key, sub := rest[:i], rest[i+1:]
			if sub == "comment" {
				s.handleComments(w, r, key)
				return
			}
		}
		s.handleIssue(w, r, rest)
	default:
		writeError(w, http.StatusNotFound, "no such endpoint: "+r.URL.Path)
	}
}

// matchFault must be called with s.mu held. It returns the next matching
// fault and decrements its Times counter, removing it when exhausted.
func (s *Server) matchFault(r *http.Request) *Fault {
	for i := range s.faults {
		f := &s.faults[i]
		if f.Method != "" && !strings.EqualFold(f.Method, r.Method) {
			continue
		}
		if f.Path != "" {
			ok, _ := path.Match(f.Path, r.URL.Path)
			if !ok && f.Path != r.URL.Path {
				continue
			}
		}
		out := *f
		f.Times--
		if f.Times <= 0 {
			s.faults = append(s.faults[:i], s.faults[i+1:]...)
		}
		return &out
	}
	return nil
}

// -----------------------------------------------------------------------------
// /myself
// -----------------------------------------------------------------------------

func (s *Server) handleMyself(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	me := s.me
	s.mu.Unlock()
	if me == nil {
		writeError(w, http.StatusUnauthorized, "no current user configured")
		return
	}
	writeJSON(w, http.StatusOK, toWireUser(s.URL(), me))
}

// -----------------------------------------------------------------------------
// /issue/{key}
// -----------------------------------------------------------------------------

func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keyDecoded, err := url.PathUnescape(key)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad key")
		return
	}
	s.mu.Lock()
	iss, ok := s.issues[keyDecoded]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "issue does not exist")
		return
	}
	writeJSON(w, http.StatusOK, toWireIssue(s.URL(), iss))
}

// -----------------------------------------------------------------------------
// /issue/{key}/comment
// -----------------------------------------------------------------------------

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keyDecoded, _ := url.PathUnescape(key)
	s.mu.Lock()
	if _, ok := s.issues[keyDecoded]; !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "issue does not exist")
		return
	}
	cs := s.comments[keyDecoded]
	out := make([]wireComment, 0, len(cs))
	for _, c := range cs {
		out = append(out, toWireComment(s.URL(), keyDecoded, c))
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, wireCommentsPage{
		Comments:   out,
		StartAt:    0,
		MaxResults: len(out),
		Total:      len(out),
	})
}

// -----------------------------------------------------------------------------
// /search/jql
// -----------------------------------------------------------------------------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	jql := q.Get("jql")
	tok := q.Get("nextPageToken")
	filt := parseJQL(jql)

	s.mu.Lock()
	all := make([]*Issue, 0, len(s.issues))
	for _, iss := range s.issues {
		if !matches(iss, filt) {
			continue
		}
		all = append(all, iss)
	}
	pageSize := s.pageSize
	base := s.URL()
	s.mu.Unlock()

	sort.Slice(all, func(i, j int) bool {
		if filt.orderDesc {
			return all[i].Updated.After(all[j].Updated)
		}
		return all[i].Updated.Before(all[j].Updated)
	})

	offset := 0
	if tok != "" {
		if n, err := strconv.Atoi(tok); err == nil && n >= 0 {
			offset = n
		}
	}
	end := offset + pageSize
	if end > len(all) {
		end = len(all)
	}
	page := all[offset:end]

	wireIssues := make([]wireIssue, 0, len(page))
	for _, iss := range page {
		wireIssues = append(wireIssues, toWireIssue(base, iss))
	}
	resp := wireSearchPage{Issues: wireIssues, IsLast: end >= len(all)}
	if !resp.IsLast {
		resp.NextPageToken = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, resp)
}

func matches(iss *Issue, f jqlFilter) bool {
	if f.project != "" && !strings.EqualFold(iss.ProjectKey, f.project) {
		return false
	}
	if f.updatedSince != nil && iss.Updated.Before(*f.updatedSince) {
		return false
	}
	return true
}

// -----------------------------------------------------------------------------
// Wire conversion
// -----------------------------------------------------------------------------

func toWireUser(base string, u *User) *wireUser {
	if u == nil {
		return nil
	}
	at := u.AccountType
	if at == "" {
		at = "atlassian"
	}
	w := &wireUser{
		AccountID:    u.AccountID,
		DisplayName:  u.DisplayName,
		EmailAddress: u.EmailAddress,
		AccountType:  at,
		Active:       true,
	}
	if u.AccountID != "" {
		w.Self = base + "/rest/api/3/user?accountId=" + url.QueryEscape(u.AccountID)
	}
	return w
}

func toWireIssue(base string, iss *Issue) wireIssue {
	prio := (*wireNamed)(nil)
	if iss.Priority != "" {
		prio = &wireNamed{Name: iss.Priority}
	}
	cat := iss.StatusCategory
	if cat == "" {
		cat = "new"
	}
	due := ""
	if !iss.DueDate.IsZero() {
		due = iss.DueDate.Format("2006-01-02")
	}
	return wireIssue{
		ID:   iss.ID,
		Self: base + "/rest/api/3/issue/" + iss.ID,
		Key:  iss.Key,
		Fields: wireIssueFields{
			Summary:     iss.Summary,
			Description: pickADF(iss.DescriptionADF, iss.Description),
			IssueType:   wireNamed{Name: defaultStr(iss.Type, "Task")},
			Status: wireStatus{
				Name:           defaultStr(iss.Status, "To Do"),
				StatusCategory: wireStatusCategory{Key: cat, Name: cat},
			},
			Priority:  prio,
			Project:   wireProject{Key: iss.ProjectKey},
			Assignee:  toWireUser(base, iss.Assignee),
			Reporter:  toWireUser(base, iss.Reporter),
			Labels:    append([]string(nil), iss.Labels...),
			DueDate:   due,
			Created:   formatJiraTime(iss.Created),
			Updated:   formatJiraTime(iss.Updated),
		},
	}
}

func toWireComment(base, issueKey string, c *Comment) wireComment {
	return wireComment{
		ID:        c.ID,
		Self:      base + "/rest/api/3/issue/" + issueKey + "/comment/" + c.ID,
		Author:    toWireUser(base, c.Author),
		Body:      pickADF(c.BodyADF, c.Body),
		Created:   formatJiraTime(c.Created),
		Updated:   formatJiraTime(c.Updated),
		JsdPublic: true,
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// formatJiraTime emits the Atlassian wire format: 2026-04-20T11:30:00.000+0000.
func formatJiraTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000-0700")
}

func defaultStr(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(wireErrorCollection{
		ErrorMessages: []string{msg},
		Status:        code,
	})
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// String returns a debug representation of the server state (issue keys, fault count).
// Useful in test failure messages.
func (s *Server) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.issues))
	for k := range s.issues {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("jiramock{issues=%v faults=%d reqs=%d}", keys, len(s.faults), len(s.requests))
}

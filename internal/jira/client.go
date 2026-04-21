// Package jira is a thin REST client for Atlassian Jira Cloud v3. It
// returns plain model.* structs and does ADF → markdown conversion so
// callers above it never see Atlassian's wire format.
package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anttti/j/internal/model"
)

// Config configures a Client.
type Config struct {
	BaseURL string // e.g. https://acme.atlassian.net
	Email   string
	Token   string
	HTTP    *http.Client
}

// Option tweaks a Client.
type Option func(*Client)

// WithSleeper replaces the retry sleep function (for tests).
func WithSleeper(f func(time.Duration)) Option { return func(c *Client) { c.sleep = f } }

// WithMaxRetries sets the maximum number of retry attempts. Default 5.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// Client talks to Jira Cloud REST v3.
type Client struct {
	base       string
	auth       string
	http       *http.Client
	sleep      func(time.Duration)
	maxRetries int
}

// New constructs a Client.
func New(cfg Config, opts ...Option) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("jira: BaseURL is required")
	}
	if cfg.Email == "" || cfg.Token == "" {
		return nil, errors.New("jira: email + token required")
	}
	httpc := cfg.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{
		base:       strings.TrimRight(cfg.BaseURL, "/"),
		auth:       "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Email+":"+cfg.Token)),
		http:       httpc,
		sleep:      time.Sleep,
		maxRetries: 5,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// IssueEntry is a decoded issue alongside its original JSON body. The raw
// bytes let the store persist the untouched payload for forward-compat.
type IssueEntry struct {
	Issue model.Issue
	Raw   json.RawMessage
}

// SearchPage is one page of search results.
type SearchPage struct {
	Entries       []IssueEntry
	NextPageToken string
	IsLast        bool
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("jira: %d %s", e.Status, strings.TrimSpace(e.Body))
}

// IsNotFound reports whether err is a 404.
func IsNotFound(err error) bool {
	var he *httpError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

// -----------------------------------------------------------------------------
// Endpoints
// -----------------------------------------------------------------------------

// Myself returns the authenticated user.
func (c *Client) Myself(ctx context.Context) (*model.User, error) {
	raw, err := c.get(ctx, "/rest/api/3/myself", nil)
	if err != nil {
		return nil, err
	}
	var w struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode myself: %w", err)
	}
	return &model.User{AccountID: w.AccountID, DisplayName: w.DisplayName, Email: w.EmailAddress}, nil
}

// Issue fetches a single issue by key.
func (c *Client) Issue(ctx context.Context, key string) (*IssueEntry, error) {
	raw, err := c.get(ctx, "/rest/api/3/issue/"+url.PathEscape(key),
		url.Values{"expand": {"renderedFields"}})
	if err != nil {
		return nil, err
	}
	return c.decodeIssue(raw)
}

// Search runs a JQL query (enhanced endpoint).
func (c *Client) Search(ctx context.Context, jql, nextPageToken string) (*SearchPage, error) {
	q := url.Values{
		"jql":    {jql},
		"fields": {"*all"},
		"expand": {"renderedFields"},
	}
	if nextPageToken != "" {
		q.Set("nextPageToken", nextPageToken)
	}
	raw, err := c.get(ctx, "/rest/api/3/search/jql", q)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Issues        []json.RawMessage `json:"issues"`
		NextPageToken string            `json:"nextPageToken"`
		IsLast        bool              `json:"isLast"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	out := &SearchPage{NextPageToken: wire.NextPageToken, IsLast: wire.IsLast}
	for _, r := range wire.Issues {
		entry, err := c.decodeIssue(r)
		if err != nil {
			return nil, err
		}
		out.Entries = append(out.Entries, *entry)
	}
	return out, nil
}

// Comments returns comments for an issue.
func (c *Client) Comments(ctx context.Context, issueKey string) ([]model.Comment, error) {
	raw, err := c.get(ctx, "/rest/api/3/issue/"+url.PathEscape(issueKey)+"/comment", nil)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Comments []struct {
			ID     string    `json:"id"`
			Author *userWire `json:"author"`
			Body   adfNode   `json:"body"`
			Created string   `json:"created"`
			Updated string   `json:"updated"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	out := make([]model.Comment, 0, len(wire.Comments))
	for _, w := range wire.Comments {
		c := model.Comment{
			ID:       w.ID,
			IssueKey: issueKey,
			Body:     renderADF(w.Body),
			Created:  parseTime(w.Created),
			Updated:  parseTime(w.Updated),
		}
		if w.Author != nil {
			c.Author = w.Author.toModel()
		}
		out = append(out, c)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// HTTP plumbing
// -----------------------------------------------------------------------------

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			c.sleep(c.backoff(attempt, lastErr))
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.auth)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			// Transport errors (refused, DNS, context cancel) aren't
			// transient in a useful way — fail fast.
			return nil, err
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		he := &httpError{Status: resp.StatusCode, Body: string(body)}
		if resp.StatusCode == http.StatusTooManyRequests {
			he.Body = resp.Header.Get("Retry-After") + ":" + he.Body
			lastErr = he
			continue
		}
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			lastErr = he
			continue
		}
		return nil, he
	}
	return nil, lastErr
}

func (c *Client) backoff(attempt int, lastErr error) time.Duration {
	// Honor Retry-After for 429s.
	var he *httpError
	if errors.As(lastErr, &he) && he.Status == http.StatusTooManyRequests {
		if idx := strings.Index(he.Body, ":"); idx > 0 {
			if secs, err := strconv.Atoi(he.Body[:idx]); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	// Exponential backoff: 250ms, 500ms, 1s, 2s, 4s.
	return time.Duration(250<<uint(attempt-1)) * time.Millisecond
}

// -----------------------------------------------------------------------------
// Issue decoding
// -----------------------------------------------------------------------------

type userWire struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

func (u *userWire) toModel() *model.User {
	if u == nil {
		return nil
	}
	return &model.User{AccountID: u.AccountID, DisplayName: u.DisplayName, Email: u.EmailAddress}
}

type issueWire struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string    `json:"summary"`
		Description adfNode   `json:"description"`
		IssueType   named     `json:"issuetype"`
		Status      statusW   `json:"status"`
		Priority    named     `json:"priority"`
		Project     keyed     `json:"project"`
		Assignee    *userWire `json:"assignee"`
		Reporter    *userWire `json:"reporter"`
		Labels      []string  `json:"labels"`
		DueDate     string    `json:"duedate"`
		Created     string    `json:"created"`
		Updated     string    `json:"updated"`
	} `json:"fields"`
}

type named struct {
	Name string `json:"name"`
}
type keyed struct {
	Key string `json:"key"`
}
type statusW struct {
	Name     string `json:"name"`
	Category struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

func (c *Client) decodeIssue(raw []byte) (*IssueEntry, error) {
	var w issueWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	iss := model.Issue{
		IssueRef:       model.IssueRef{Key: w.Key, ID: w.ID, ProjectKey: w.Fields.Project.Key},
		Summary:        w.Fields.Summary,
		Description:    renderADF(w.Fields.Description),
		Type:           w.Fields.IssueType.Name,
		Status:         w.Fields.Status.Name,
		StatusCategory: w.Fields.Status.Category.Key,
		Priority:       w.Fields.Priority.Name,
		Assignee:       w.Fields.Assignee.toModel(),
		Reporter:       w.Fields.Reporter.toModel(),
		Labels:         w.Fields.Labels,
		Created:        parseTime(w.Fields.Created),
		Updated:        parseTime(w.Fields.Updated),
		URL:            c.base + "/browse/" + w.Key,
	}
	if w.Fields.DueDate != "" {
		if t, err := time.Parse("2006-01-02", w.Fields.DueDate); err == nil {
			iss.DueDate = &t
		}
	}
	return &IssueEntry{Issue: iss, Raw: raw}, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

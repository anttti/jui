// Package sync is the incremental Jira → store engine. It holds no state
// of its own beyond injected dependencies; all persistent state lives in
// the store.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/anttti/j/internal/jira"
	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
)

// JiraClient is the subset of jira.Client the engine needs.
type JiraClient interface {
	Search(ctx context.Context, jql, nextPageToken string) (*jira.SearchPage, error)
	Issue(ctx context.Context, key string) (*jira.IssueEntry, error)
	Comments(ctx context.Context, key string) ([]model.Comment, error)
}

// IssueFetcher is what the TUI uses for on-demand jump-to-key. It is
// implemented by *Engine.FetchOne.
type IssueFetcher interface {
	FetchOne(ctx context.Context, key string) error
}

// Store is the read+write surface sync needs.
type Store interface {
	store.Reader
	store.Writer
}

// Engine runs sync cycles.
type Engine struct {
	jira            JiraClient
	store           Store
	clock           func() time.Time
	initialLookback time.Duration
}

// Option tweaks an Engine.
type Option func(*Engine)

// WithClock sets the clock used for "now" (useful for deterministic tests).
func WithClock(f func() time.Time) Option { return func(e *Engine) { e.clock = f } }

// WithInitialLookback sets how far back the initial sync reaches. Default 90d.
func WithInitialLookback(d time.Duration) Option { return func(e *Engine) { e.initialLookback = d } }

// New constructs an Engine.
func New(jc JiraClient, s Store, opts ...Option) *Engine {
	e := &Engine{
		jira:            jc,
		store:           s,
		clock:           time.Now,
		initialLookback: 90 * 24 * time.Hour,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run performs a single sync cycle against baseJQL.
func (e *Engine) Run(ctx context.Context, baseJQL string) error {
	state, err := e.store.SyncState(ctx)
	if err != nil {
		return fmt.Errorf("read sync state: %w", err)
	}

	since := state.LastSyncUTC
	if since.IsZero() {
		since = e.clock().Add(-e.initialLookback)
	}
	startedAt := e.clock()
	jql := fmt.Sprintf(`(%s) AND updated >= %q ORDER BY updated ASC`,
		baseJQL, since.Format("2006-01-02 15:04"))

	maxUpdated := since
	var token string
	for {
		page, err := e.jira.Search(ctx, jql, token)
		if err != nil {
			// Preserve prior watermark; record the error.
			_ = e.store.SetSyncState(ctx, model.SyncState{
				LastSyncUTC:    state.LastSyncUTC,
				LastFullSyncAt: state.LastFullSyncAt,
				LastError:      err.Error(),
			})
			return fmt.Errorf("search: %w", err)
		}
		for _, entry := range page.Entries {
			if err := e.store.UpsertIssue(ctx, entry.Issue, entry.Raw); err != nil {
				return fmt.Errorf("upsert %s: %w", entry.Issue.Key, err)
			}
			cs, err := e.jira.Comments(ctx, entry.Issue.Key)
			if err != nil {
				return fmt.Errorf("comments %s: %w", entry.Issue.Key, err)
			}
			if err := e.store.ReplaceComments(ctx, entry.Issue.Key, cs); err != nil {
				return fmt.Errorf("replace comments %s: %w", entry.Issue.Key, err)
			}
			if entry.Issue.Updated.After(maxUpdated) {
				maxUpdated = entry.Issue.Updated
			}
		}
		if page.IsLast || page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}

	// Advance watermark. If we saw no rows, use startedAt so the next run
	// asks for issues updated after the query time rather than replaying
	// the same lookback window.
	newWatermark := maxUpdated
	if newWatermark.Equal(since) {
		newWatermark = startedAt
	}
	return e.store.SetSyncState(ctx, model.SyncState{
		LastSyncUTC:    newWatermark,
		LastFullSyncAt: startedAt,
		LastError:      "",
	})
}

// FetchOne fetches a single issue plus its comments and upserts them. Used
// by the TUI for :KEY lookups when the issue is not already cached.
func (e *Engine) FetchOne(ctx context.Context, key string) error {
	entry, err := e.jira.Issue(ctx, key)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", key, err)
	}
	if err := e.store.UpsertIssue(ctx, entry.Issue, entry.Raw); err != nil {
		return fmt.Errorf("upsert %s: %w", key, err)
	}
	cs, err := e.jira.Comments(ctx, key)
	if err != nil {
		return fmt.Errorf("comments %s: %w", key, err)
	}
	return e.store.ReplaceComments(ctx, key, cs)
}

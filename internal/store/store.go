// Package store defines the data-access contract used by everything above
// the database layer. Any frontend (TUI, future web UI, etc.) depends only
// on these interfaces.
package store

import (
	"context"
	"io"

	"github.com/anttti/j/internal/model"
)

// Reader is the read-only surface, sufficient to build a browsing UI.
type Reader interface {
	// List returns issues matching f, paged by p, newest Updated first.
	// The second return is the total count matching f (independent of p).
	List(ctx context.Context, f model.Filter, p model.Page) ([]model.Issue, int, error)
	// Get returns the issue with the given key, or (nil, nil) if absent.
	// Key match is case-insensitive.
	Get(ctx context.Context, key string) (*model.Issue, error)
	// Comments returns comments on an issue in Created ascending order.
	// Unknown key returns (nil, nil).
	Comments(ctx context.Context, key string) ([]model.Comment, error)
	DistinctTypes(ctx context.Context) ([]string, error)
	DistinctStatuses(ctx context.Context) ([]string, error)
	// Assignees returns distinct assignees present in the store.
	Assignees(ctx context.Context) ([]model.User, error)
	SyncState(ctx context.Context) (model.SyncState, error)
}

// Writer is the surface the sync engine uses.
type Writer interface {
	UpsertIssue(ctx context.Context, iss model.Issue, rawJSON []byte) error
	// ReplaceComments sets the comments of issueKey to exactly the given
	// slice. Existing comments for that issue are removed first.
	ReplaceComments(ctx context.Context, issueKey string, comments []model.Comment) error
	SetSyncState(ctx context.Context, s model.SyncState) error
}

// Store combines read and write access with a resource lifecycle.
type Store interface {
	Reader
	Writer
	io.Closer
}

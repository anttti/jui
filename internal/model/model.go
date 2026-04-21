// Package model contains plain domain structs shared across the data core and
// any UI. It must not import anything from tui, sqlite drivers, or bubbletea.
package model

import "time"

// IssueRef is the minimum identity needed to address an issue.
type IssueRef struct {
	Key        string
	ID         string
	ProjectKey string
}

// User represents a Jira user (assignee/reporter/comment author).
type User struct {
	AccountID   string
	DisplayName string
	Email       string
}

// Issue is a Jira issue in a UI-ready shape. Description is markdown.
type Issue struct {
	IssueRef
	Summary        string
	Description    string
	Status         string
	StatusCategory string // todo | indeterminate | done
	Type           string
	Priority       string
	Assignee       *User
	Reporter       *User
	Labels         []string
	DueDate        *time.Time
	Created        time.Time
	Updated        time.Time
	URL            string
}

// Unassigned reports whether the issue has no assignee.
func (i Issue) Unassigned() bool { return i.Assignee == nil }

// Comment is a single comment on an issue. Body is markdown.
type Comment struct {
	ID       string
	IssueKey string
	Author   *User
	Body     string
	Created  time.Time
	Updated  time.Time
}

// AssigneeKind enumerates the shapes of an assignee filter.
type AssigneeKind int

const (
	AssigneeKindAll AssigneeKind = iota
	AssigneeKindMe
	AssigneeKindUnassigned
	AssigneeKindAccount
)

// AssigneeFilter scopes a list query by assignee.
type AssigneeFilter struct {
	Kind      AssigneeKind
	AccountID string // only used when Kind == AssigneeKindAccount
}

func AssigneeAll() AssigneeFilter        { return AssigneeFilter{Kind: AssigneeKindAll} }
func AssigneeMe() AssigneeFilter         { return AssigneeFilter{Kind: AssigneeKindMe} }
func AssigneeUnassigned() AssigneeFilter { return AssigneeFilter{Kind: AssigneeKindUnassigned} }
func AssigneeAccount(id string) AssigneeFilter {
	return AssigneeFilter{Kind: AssigneeKindAccount, AccountID: id}
}

// Filter narrows a list query. Empty slices / AssigneeAll mean "no filter".
type Filter struct {
	Types    []string
	Statuses []string
	Assignee AssigneeFilter
	Search   string
}

// Empty reports whether the filter is a no-op.
func (f Filter) Empty() bool {
	return len(f.Types) == 0 &&
		len(f.Statuses) == 0 &&
		f.Assignee.Kind == AssigneeKindAll &&
		f.Search == ""
}

// Page is a simple limit/offset pager.
type Page struct {
	Limit  int
	Offset int
}

// SyncState is the background daemon's persistent watermark.
type SyncState struct {
	LastSyncUTC    time.Time
	LastFullSyncAt time.Time
	LastError      string
}

// NeedsInitialSync reports whether no sync has ever completed.
func (s SyncState) NeedsInitialSync() bool { return s.LastSyncUTC.IsZero() }

package list

import (
	"sort"
	"strings"

	"github.com/anttti/j/internal/model"
)

// SortKey is one level of a multi-key sort order applied to the list.
type SortKey struct {
	Column ColumnID
	Desc   bool
}

// SortableColumns lists columns that may appear in a sort key. Not every
// column is meaningful to sort by (e.g. the raw summary text).
var SortableColumns = []ColumnID{
	ColKey, ColType, ColStatus, ColPrio, ColAssignee, ColSummary, ColUpdated, ColCreated,
}

// applySort sorts issues in place according to the given ordered keys. It is
// stable so earlier keys dominate.
func applySort(issues []model.Issue, keys []SortKey) {
	if len(keys) == 0 {
		return
	}
	sort.SliceStable(issues, func(i, j int) bool {
		for _, k := range keys {
			c := compareByColumn(issues[i], issues[j], k.Column)
			if k.Desc {
				c = -c
			}
			if c != 0 {
				return c < 0
			}
		}
		return false
	})
}

func compareByColumn(a, b model.Issue, col ColumnID) int {
	switch col {
	case ColKey:
		return cmpString(a.Key, b.Key)
	case ColType:
		return cmpString(a.Type, b.Type)
	case ColStatus:
		return cmpString(a.Status, b.Status)
	case ColPrio:
		return cmpInt(priorityRank(a.Priority), priorityRank(b.Priority))
	case ColAssignee:
		return cmpString(assigneeName(a), assigneeName(b))
	case ColSummary:
		return cmpString(a.Summary, b.Summary)
	case ColUpdated:
		if a.Updated.Equal(b.Updated) {
			return 0
		}
		if a.Updated.Before(b.Updated) {
			return -1
		}
		return 1
	case ColCreated:
		if a.Created.Equal(b.Created) {
			return 0
		}
		if a.Created.Before(b.Created) {
			return -1
		}
		return 1
	default:
		return 0
	}
}

func cmpString(a, b string) int {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func priorityRank(p string) int {
	switch strings.ToLower(p) {
	case "highest":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	case "lowest":
		return 4
	default:
		return 5
	}
}

func assigneeName(iss model.Issue) string {
	if iss.Assignee == nil {
		return ""
	}
	return iss.Assignee.DisplayName
}

// toggleSortKey mutates the sort stack to reflect a Space-press on column col.
// The cycle per-column is: absent → asc → desc → absent.
func toggleSortKey(keys []SortKey, col ColumnID) []SortKey {
	idx := -1
	for i, k := range keys {
		if k.Column == col {
			idx = i
			break
		}
	}
	switch {
	case idx == -1:
		return append(keys, SortKey{Column: col, Desc: false})
	case !keys[idx].Desc:
		keys[idx].Desc = true
		return keys
	default:
		return append(keys[:idx], keys[idx+1:]...)
	}
}

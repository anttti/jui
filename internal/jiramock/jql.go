package jiramock

import (
	"regexp"
	"strings"
	"time"
)

// jqlFilter is a permissive parse of the JQL clauses this codebase emits.
// Anything we don't recognise is silently ignored so tests stay loose.
type jqlFilter struct {
	project      string
	updatedSince *time.Time
	orderDesc    bool
}

var (
	rxProject = regexp.MustCompile(`(?i)\bproject\s*=\s*"?([A-Z][A-Z0-9_]*)"?`)
	rxUpdated = regexp.MustCompile(`(?i)\bupdated\s*>=\s*"([^"]+)"`)
	rxOrder   = regexp.MustCompile(`(?i)\bORDER\s+BY\s+updated\s+(ASC|DESC)\b`)
)

func parseJQL(s string) jqlFilter {
	f := jqlFilter{}
	if m := rxProject.FindStringSubmatch(s); m != nil {
		f.project = strings.ToUpper(m[1])
	}
	if m := rxUpdated.FindStringSubmatch(s); m != nil {
		// Jira accepts both "2006-01-02 15:04" and ISO forms; we support
		// the same set as the production client.
		layouts := []string{
			"2006-01-02 15:04",
			"2006-01-02T15:04:05.000-0700",
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02",
		}
		for _, l := range layouts {
			if t, err := time.Parse(l, m[1]); err == nil {
				tt := t.UTC()
				f.updatedSince = &tt
				break
			}
		}
	}
	if m := rxOrder.FindStringSubmatch(s); m != nil {
		f.orderDesc = strings.EqualFold(m[1], "DESC")
	}
	return f
}

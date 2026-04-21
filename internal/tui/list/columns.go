package list

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/tui/style"
)

// ColumnID is a stable identifier for a renderable column in the list.
type ColumnID string

const (
	ColKey      ColumnID = "key"
	ColType     ColumnID = "type"
	ColStatus   ColumnID = "status"
	ColPrio     ColumnID = "prio"
	ColAssignee ColumnID = "assignee"
	ColSummary  ColumnID = "summary"
	ColUpdated  ColumnID = "updated"
	ColCreated  ColumnID = "created"
	ColReporter ColumnID = "reporter"
)

// DefaultColumns is the built-in column order shown when no preset is active.
var DefaultColumns = []ColumnID{
	ColKey, ColType, ColStatus, ColPrio, ColAssignee, ColSummary, ColUpdated,
}

// AllColumns is the column order shown in the column-configuration chip.
var AllColumns = []ColumnID{
	ColKey, ColType, ColStatus, ColPrio, ColAssignee, ColReporter, ColSummary, ColUpdated, ColCreated,
}

// column holds per-column metadata and cell rendering.
type column struct {
	id    ColumnID
	label string
	width int // 0 means "flex" (takes remaining space); at most one per row
	align lipgloss.Position
	// value returns a rendering style and display text for the cell.
	value func(iss model.Issue) (lipgloss.Style, string)
}

var columnTable = map[ColumnID]column{
	ColKey: {
		id: ColKey, label: "KEY", width: 10, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return lipgloss.NewStyle(), iss.Key
		},
	},
	ColType: {
		id: ColType, label: "T", width: 2, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return style.TypeStyle(iss.Type), style.TypeSymbol(iss.Type)
		},
	},
	ColStatus: {
		id: ColStatus, label: "STATUS", width: 14, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return style.StatusStyle(iss.StatusCategory), iss.Status
		},
	},
	ColPrio: {
		id: ColPrio, label: "PRIO", width: 8, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			p := iss.Priority
			if p == "" {
				p = "—"
			}
			return style.PriorityStyle(iss.Priority), p
		},
	},
	ColAssignee: {
		id: ColAssignee, label: "ASG", width: 4, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			if iss.Assignee == nil || iss.Assignee.DisplayName == "" {
				return style.MutedText, "—"
			}
			c := style.AssigneeColor(iss.Assignee.AccountID)
			return lipgloss.NewStyle().Foreground(c).Bold(true), style.Initials(iss.Assignee.DisplayName)
		},
	},
	ColReporter: {
		id: ColReporter, label: "REP", width: 4, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			if iss.Reporter == nil || iss.Reporter.DisplayName == "" {
				return style.MutedText, "—"
			}
			c := style.AssigneeColor(iss.Reporter.AccountID)
			return lipgloss.NewStyle().Foreground(c), style.Initials(iss.Reporter.DisplayName)
		},
	},
	ColSummary: {
		id: ColSummary, label: "SUMMARY", width: 0, align: lipgloss.Left,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return lipgloss.NewStyle(), iss.Summary
		},
	},
	ColUpdated: {
		id: ColUpdated, label: "UPD", width: 6, align: lipgloss.Right,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return style.MutedText, relTime(iss.Updated)
		},
	},
	ColCreated: {
		id: ColCreated, label: "CRT", width: 6, align: lipgloss.Right,
		value: func(iss model.Issue) (lipgloss.Style, string) {
			return style.MutedText, relTime(iss.Created)
		},
	},
}

// columnDef returns the definition for id, or the Key column as a safe default.
func columnDef(id ColumnID) column {
	if c, ok := columnTable[id]; ok {
		return c
	}
	return columnTable[ColKey]
}

// columnLabel returns the display label for the column id.
func columnLabel(id ColumnID) string { return columnDef(id).label }

// computeColumnWidths returns the rendered widths for the given ordered set of
// columns, fitting within total width w. Exactly one flex column (width 0)
// absorbs the remainder; if there is none, widths match their intrinsic values
// (content may overflow w, which is then truncated at the row level).
func computeColumnWidths(cols []ColumnID, w int) []int {
	widths := make([]int, len(cols))
	fixed := 0
	flexIdx := -1
	for i, id := range cols {
		cd := columnDef(id)
		if cd.width == 0 && flexIdx == -1 {
			flexIdx = i
			continue
		}
		widths[i] = cd.width
		fixed += cd.width
	}
	sep := 0
	if len(cols) > 1 {
		sep = len(cols) - 1
	}
	if flexIdx >= 0 {
		remaining := w - fixed - sep
		if remaining < 0 {
			remaining = 0
		}
		widths[flexIdx] = remaining
	}
	return widths
}

// truncateEllipsis shortens s so that its display width is at most w, adding
// an ellipsis when truncation happened.
func truncateEllipsis(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// renderCell produces a single cell with fixed width, honoring selection
// background and internal ANSI-code safety.
func renderCell(base lipgloss.Style, width int, align lipgloss.Position, text string, selected bool) string {
	if width <= 0 {
		return ""
	}
	s := base.Copy().Width(width).MaxWidth(width).Align(align).Inline(true)
	if selected {
		s = s.Background(style.SelectedBg)
	}
	return s.Render(truncateEllipsis(text, width))
}

// renderHeaderLine returns the column-header row for the given columns + width.
func renderHeaderLine(cols []ColumnID, w int, sortKeys []SortKey) string {
	widths := computeColumnWidths(cols, w)
	sortMap := map[ColumnID]SortKey{}
	for _, sk := range sortKeys {
		sortMap[sk.Column] = sk
	}
	parts := make([]string, 0, len(cols))
	for i, id := range cols {
		label := columnLabel(id)
		if sk, ok := sortMap[id]; ok {
			arrow := "↑"
			if sk.Desc {
				arrow = "↓"
			}
			label = label + arrow
		}
		parts = append(parts, renderCell(style.MutedText, widths[i], columnDef(id).align, label, false))
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(parts, " "))
}

// renderIssueRow renders one issue using the given ordered columns.
func renderIssueRow(cols []ColumnID, widths []int, iss model.Issue, w int, selected bool) string {
	parts := make([]string, 0, len(cols))
	for i, id := range cols {
		cd := columnDef(id)
		s, text := cd.value(iss)
		// KEY gets primary color when selected.
		if selected && id == ColKey {
			s = s.Copy().Foreground(style.Primary).Bold(true)
		}
		parts = append(parts, renderCell(s, widths[i], cd.align, text, selected))
	}
	sep := " "
	if selected {
		sep = lipgloss.NewStyle().Background(style.SelectedBg).Render(" ")
	}
	row := strings.Join(parts, sep)
	if selected {
		if rw := lipgloss.Width(row); rw < w {
			row += lipgloss.NewStyle().Background(style.SelectedBg).Render(strings.Repeat(" ", w-rw))
		}
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(row)
}

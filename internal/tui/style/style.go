// Package style is the single source of truth for TUI colors, borders, and
// reusable lipgloss.Styles. Both list and detail views import it; no other
// package creates styles ad-hoc.
package style

import (
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// -----------------------------------------------------------------------------
// Palette
// -----------------------------------------------------------------------------

var (
	Primary    = lipgloss.AdaptiveColor{Light: "#9D4EDD", Dark: "#C586C0"}
	Subtle     = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#7A7A8C"}
	Muted      = lipgloss.AdaptiveColor{Light: "#4A4A4A", Dark: "#9A9AAE"}
	Border     = lipgloss.AdaptiveColor{Light: "#C7C7D1", Dark: "#3B3B4F"}
	Success    = lipgloss.AdaptiveColor{Light: "#1E8E3E", Dark: "#73D99C"}
	Warning    = lipgloss.AdaptiveColor{Light: "#B06E00", Dark: "#E6C06C"}
	Danger     = lipgloss.AdaptiveColor{Light: "#C5221F", Dark: "#F08A8A"}
	Info       = lipgloss.AdaptiveColor{Light: "#1A73E8", Dark: "#7DB4F5"}
	SelectedBg = lipgloss.AdaptiveColor{Light: "#E9DDF4", Dark: "#2A2438"}
	BarBg      = lipgloss.AdaptiveColor{Light: "#E1E1E8", Dark: "#1A1A24"}
)

// -----------------------------------------------------------------------------
// Text styles
// -----------------------------------------------------------------------------

var (
	Title        = lipgloss.NewStyle().Bold(true).Foreground(Primary)
	Subtitle     = lipgloss.NewStyle().Bold(true).Foreground(Muted)
	MutedText    = lipgloss.NewStyle().Foreground(Muted)
	SubtleText   = lipgloss.NewStyle().Foreground(Subtle)
	Error        = lipgloss.NewStyle().Foreground(Danger)
	Tab          = lipgloss.NewStyle().Padding(0, 1).Foreground(Muted)
	TabFocused   = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(Primary)
	TabSeparator = lipgloss.NewStyle().Foreground(Border).Render("│")
)

// -----------------------------------------------------------------------------
// Layout styles
// -----------------------------------------------------------------------------

var (
	Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Border).
		Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Background(BarBg).
			Foreground(Muted)

	SelectedRow = lipgloss.NewStyle().
			Background(SelectedBg).
			Bold(true)
)

// -----------------------------------------------------------------------------
// Semantic helpers
// -----------------------------------------------------------------------------

// StatusStyle picks a color by the Jira status category.
// Categories: "done", "indeterminate", "todo" (anything else → Muted).
func StatusStyle(category string) lipgloss.Style {
	switch strings.ToLower(category) {
	case "done":
		return lipgloss.NewStyle().Foreground(Success)
	case "indeterminate":
		return lipgloss.NewStyle().Foreground(Warning)
	case "todo", "new":
		return lipgloss.NewStyle().Foreground(Info)
	default:
		return lipgloss.NewStyle().Foreground(Muted)
	}
}

// PriorityStyle picks a color by Jira priority name.
func PriorityStyle(priority string) lipgloss.Style {
	switch strings.ToLower(priority) {
	case "highest", "high":
		return lipgloss.NewStyle().Foreground(Danger).Bold(true)
	case "medium":
		return lipgloss.NewStyle().Foreground(Warning)
	case "low", "lowest":
		return lipgloss.NewStyle().Foreground(Info)
	default:
		return lipgloss.NewStyle().Foreground(Muted)
	}
}

// TypeStyle picks a color by Jira issue type.
func TypeStyle(typ string) lipgloss.Style {
	switch strings.ToLower(typ) {
	case "bug":
		return lipgloss.NewStyle().Foreground(Danger).Bold(true)
	case "story":
		return lipgloss.NewStyle().Foreground(Success).Bold(true)
	case "epic":
		return lipgloss.NewStyle().Foreground(Primary).Bold(true)
	case "task", "sub-task", "subtask":
		return lipgloss.NewStyle().Foreground(Info).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(Muted).Bold(true)
	}
}

// ModePill renders a mode indicator as a colored, padded block.
func ModePill(mode string) string {
	var bg lipgloss.TerminalColor
	switch strings.ToUpper(mode) {
	case "INSERT":
		bg = Warning
	case "COMMAND":
		bg = Info
	default:
		bg = Primary
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#111111")).
		Background(bg).
		Padding(0, 1).
		Render(mode)
}

// TypeSymbol returns a single-width glyph for the issue type.
func TypeSymbol(typ string) string {
	switch strings.ToLower(typ) {
	case "bug":
		return "■"
	case "story":
		return "◆"
	case "epic":
		return "★"
	case "task":
		return "▲"
	case "sub-task", "subtask":
		return "↳"
	default:
		return "●"
	}
}

// assigneePalette is a curated set of visually distinct accent colors used to
// color assignee initials. Sampled per-account so the same person renders the
// same color across rows.
var assigneePalette = []lipgloss.Color{
	"#E06C75", "#98C379", "#E5C07B", "#61AFEF",
	"#C678DD", "#56B6C2", "#D19A66", "#BE5046",
	"#A1D8B2", "#F5A3C7", "#7DCFFF", "#FFB86C",
}

// AssigneeColor picks a stable color from the palette for a given account id.
func AssigneeColor(id string) lipgloss.Color {
	if id == "" {
		return lipgloss.Color("#7A7A8C")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return assigneePalette[int(h.Sum32())%len(assigneePalette)]
}

// Initials returns up to two uppercase initials extracted from a display name.
// Falls back to "?" for empty input.
func Initials(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	fields := strings.Fields(name)
	pick := func(s string) rune {
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToUpper(r)
			}
		}
		return 0
	}
	var out []rune
	if len(fields) > 0 {
		if r := pick(fields[0]); r != 0 {
			out = append(out, r)
		}
	}
	if len(fields) > 1 {
		if r := pick(fields[len(fields)-1]); r != 0 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// HorizontalRule returns a muted horizontal line of the given width.
func HorizontalRule(width int) string {
	if width <= 0 {
		width = 1
	}
	return lipgloss.NewStyle().Foreground(Border).Render(strings.Repeat("─", width))
}

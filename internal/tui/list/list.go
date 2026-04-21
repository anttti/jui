// Package list is the Bubble Tea model for the issue list view.
package list

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
)

// Mode is the vim-style mode this view is in.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
)

// Chip identifies which toolbar filter is focused, if any.
type Chip int

const (
	ChipNone Chip = iota
	ChipType
	ChipStatus
	ChipAssignee
)

// Clipboard abstracts OS clipboard access for testability.
type Clipboard interface{ Copy(string) error }

// Option tweaks a Model.
type Option func(*Model)

// WithClipboard injects a clipboard.
func WithClipboard(c Clipboard) Option { return func(m *Model) { m.clip = c } }

// -----------------------------------------------------------------------------
// Messages
// -----------------------------------------------------------------------------

// loadedMsg is the result of a reload Cmd.
type loadedMsg struct {
	issues []model.Issue
	total  int
	err    error
}

// OpenIssueMsg is emitted when the user opens an issue (Enter on a row or
// :KEY<Enter> in command mode).
type OpenIssueMsg struct{ Key string }

// OpenURLMsg is emitted when the user presses `o` on a row.
type OpenURLMsg struct{ URL string }

// -----------------------------------------------------------------------------
// Model
// -----------------------------------------------------------------------------

// Model is the list view.
type Model struct {
	reader store.Reader
	clip   Clipboard

	mode      Mode
	filter    model.Filter
	searchBuf string
	cmdBuf    string
	chipFocus Chip

	// per-chip UI state
	typeValues      []string
	typeIdx         int
	typeSelected    map[string]bool
	statusValues    []string
	statusIdx       int
	statusSelected  map[string]bool
	assigneeValues  []model.User
	assigneeIdx     int

	issues []model.Issue
	total  int
	cursor int

	pending string

	err error

	width, height int
}

// New creates a list Model wired to the given reader.
func New(r store.Reader, opts ...Option) Model {
	m := Model{
		reader:         r,
		typeSelected:   map[string]bool{},
		statusSelected: map[string]bool{},
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// Init returns the initial load command.
func (m Model) Init() tea.Cmd { return m.reload() }

// Update advances state in response to a message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.issues = msg.issues
		m.total = msg.total
		m.err = msg.err
		if m.cursor > len(m.issues)-1 {
			m.cursor = maxInt(0, len(m.issues)-1)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// -----------------------------------------------------------------------------
// Key handling
// -----------------------------------------------------------------------------

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.mode {
	case ModeInsert:
		return m.handleInsert(msg)
	case ModeCommand:
		return m.handleCommand(msg)
	default:
		if m.chipFocus != ChipNone {
			return m.handleChip(msg)
		}
		return m.handleNormal(msg)
	}
}

func (m Model) handleNormal(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Multi-key sequences (gg, yy).
	if m.pending != "" {
		combined := m.pending + msg.String()
		m.pending = ""
		switch combined {
		case "gg":
			m.cursor = 0
			return m, nil
		case "yy":
			return m.yankCurrent()
		}
		// Fall through to handle the second key on its own if the
		// sequence didn't match.
	}

	switch msg.Type {
	case tea.KeyCtrlD:
		m.cursor = clamp(m.cursor+halfPage(m.height), 0, len(m.issues)-1)
		return m, nil
	case tea.KeyCtrlU:
		m.cursor = clamp(m.cursor-halfPage(m.height), 0, len(m.issues)-1)
		return m, nil
	case tea.KeyCtrlF:
		m.cursor = clamp(m.cursor+pageSize(m.height), 0, len(m.issues)-1)
		return m, nil
	case tea.KeyCtrlB:
		m.cursor = clamp(m.cursor-pageSize(m.height), 0, len(m.issues)-1)
		return m, nil
	case tea.KeyUp:
		m.cursor = clamp(m.cursor-1, 0, len(m.issues)-1)
		return m, nil
	case tea.KeyDown:
		m.cursor = clamp(m.cursor+1, 0, len(m.issues)-1)
		return m, nil
	case tea.KeyEnter:
		if m.cursor < len(m.issues) {
			key := m.issues[m.cursor].Key
			return m, func() tea.Msg { return OpenIssueMsg{Key: key} }
		}
		return m, nil
	case tea.KeyEsc:
		return m, nil
	}

	if len(msg.Runes) != 1 {
		return m, nil
	}
	switch msg.Runes[0] {
	case 'j':
		m.cursor = clamp(m.cursor+1, 0, len(m.issues)-1)
	case 'k':
		m.cursor = clamp(m.cursor-1, 0, len(m.issues)-1)
	case 'G':
		if len(m.issues) > 0 {
			m.cursor = len(m.issues) - 1
		}
	case 'g', 'y':
		m.pending = string(msg.Runes[0])
	case 'l':
		if m.cursor < len(m.issues) {
			key := m.issues[m.cursor].Key
			return m, func() tea.Msg { return OpenIssueMsg{Key: key} }
		}
	case '/':
		m.mode = ModeInsert
		m.searchBuf = ""
	case ':':
		m.mode = ModeCommand
		m.cmdBuf = ""
	case 't':
		return m.focusChip(ChipType)
	case 's':
		return m.focusChip(ChipStatus)
	case 'a':
		return m.focusChip(ChipAssignee)
	case 'o':
		if m.cursor < len(m.issues) {
			url := m.issues[m.cursor].URL
			return m, func() tea.Msg { return OpenURLMsg{URL: url} }
		}
	}
	return m, nil
}

func (m Model) handleInsert(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		m.searchBuf = ""
		m.filter.Search = ""
		return m, m.reload()
	case tea.KeyEnter:
		m.mode = ModeNormal
		m.filter.Search = m.searchBuf
		return m, m.reload()
	case tea.KeyBackspace:
		if len(m.searchBuf) > 0 {
			m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
			m.filter.Search = m.searchBuf
			return m, m.reload()
		}
		return m, nil
	}
	if len(msg.Runes) > 0 {
		m.searchBuf += string(msg.Runes)
		m.filter.Search = m.searchBuf
		return m, m.reload()
	}
	return m, nil
}

func (m Model) handleCommand(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		m.cmdBuf = ""
		return m, nil
	case tea.KeyEnter:
		cmdText := m.cmdBuf
		m.mode = ModeNormal
		m.cmdBuf = ""
		return m.runCommand(cmdText)
	case tea.KeyBackspace:
		if len(m.cmdBuf) > 0 {
			m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
		}
		return m, nil
	}
	if len(msg.Runes) > 0 {
		m.cmdBuf += string(msg.Runes)
	}
	return m, nil
}

func (m Model) runCommand(text string) (Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}
	if looksLikeIssueKey(text) {
		key := strings.ToUpper(text)
		return m, func() tea.Msg { return OpenIssueMsg{Key: key} }
	}
	// Unknown command → silently ignore for now.
	return m, nil
}

// -----------------------------------------------------------------------------
// Chip focus handling
// -----------------------------------------------------------------------------

func (m Model) focusChip(c Chip) (Model, tea.Cmd) {
	m.chipFocus = c
	ctx := context.Background()
	switch c {
	case ChipType:
		vs, _ := m.reader.DistinctTypes(ctx)
		m.typeValues = vs
		m.typeIdx = 0
		if m.typeSelected == nil {
			m.typeSelected = map[string]bool{}
		}
	case ChipStatus:
		vs, _ := m.reader.DistinctStatuses(ctx)
		m.statusValues = vs
		m.statusIdx = 0
		if m.statusSelected == nil {
			m.statusSelected = map[string]bool{}
		}
	case ChipAssignee:
		us, _ := m.reader.Assignees(ctx)
		m.assigneeValues = us
		m.assigneeIdx = 0
	}
	return m, nil
}

func (m Model) handleChip(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.chipFocus = ChipNone
		return m, nil
	case tea.KeyEnter:
		// Commit current selection into the filter and unfocus.
		switch m.chipFocus {
		case ChipType:
			m.filter.Types = keysTrue(m.typeSelected)
			sort.Strings(m.filter.Types)
		case ChipStatus:
			m.filter.Statuses = keysTrue(m.statusSelected)
			sort.Strings(m.filter.Statuses)
		case ChipAssignee:
			if m.assigneeIdx >= 0 && m.assigneeIdx < len(m.assigneeValues) {
				m.filter.Assignee = model.AssigneeAccount(m.assigneeValues[m.assigneeIdx].AccountID)
			} else {
				m.filter.Assignee = model.AssigneeAll()
			}
		}
		m.chipFocus = ChipNone
		return m, m.reload()
	case tea.KeySpace:
		switch m.chipFocus {
		case ChipType:
			if m.typeIdx < len(m.typeValues) {
				v := m.typeValues[m.typeIdx]
				m.typeSelected[v] = !m.typeSelected[v]
			}
		case ChipStatus:
			if m.statusIdx < len(m.statusValues) {
				v := m.statusValues[m.statusIdx]
				m.statusSelected[v] = !m.statusSelected[v]
			}
		}
		return m, nil
	}
	if len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'j':
			switch m.chipFocus {
			case ChipType:
				m.typeIdx = clamp(m.typeIdx+1, 0, len(m.typeValues)-1)
			case ChipStatus:
				m.statusIdx = clamp(m.statusIdx+1, 0, len(m.statusValues)-1)
			case ChipAssignee:
				m.assigneeIdx = clamp(m.assigneeIdx+1, 0, len(m.assigneeValues)-1)
			}
		case 'k':
			switch m.chipFocus {
			case ChipType:
				m.typeIdx = clamp(m.typeIdx-1, 0, len(m.typeValues)-1)
			case ChipStatus:
				m.statusIdx = clamp(m.statusIdx-1, 0, len(m.statusValues)-1)
			case ChipAssignee:
				m.assigneeIdx = clamp(m.assigneeIdx-1, 0, len(m.assigneeValues)-1)
			}
		}
	}
	return m, nil
}

// -----------------------------------------------------------------------------
// Actions
// -----------------------------------------------------------------------------

func (m Model) yankCurrent() (Model, tea.Cmd) {
	if m.cursor >= len(m.issues) {
		return m, nil
	}
	if m.clip != nil {
		_ = m.clip.Copy(m.issues[m.cursor].Key)
	}
	return m, nil
}

// -----------------------------------------------------------------------------
// Data loading
// -----------------------------------------------------------------------------

func (m Model) reload() tea.Cmd {
	reader := m.reader
	filter := m.filter
	return func() tea.Msg {
		issues, total, err := reader.List(context.Background(), filter, model.Page{Limit: 500})
		return loadedMsg{issues: issues, total: total, err: err}
	}
}

// -----------------------------------------------------------------------------
// View
// -----------------------------------------------------------------------------

// View renders the list. Simple for now; teatest golden coverage comes later.
func (m Model) View() string {
	var b strings.Builder

	// Toolbar.
	b.WriteString(renderChip("t", "type", summariseSelected(m.typeSelected), m.chipFocus == ChipType))
	b.WriteString("  ")
	b.WriteString(renderChip("s", "status", summariseSelected(m.statusSelected), m.chipFocus == ChipStatus))
	b.WriteString("  ")
	b.WriteString(renderChip("a", "assignee", assigneeLabel(m.filter.Assignee), m.chipFocus == ChipAssignee))
	b.WriteString("   ")
	if m.mode == ModeInsert {
		b.WriteString("/" + m.searchBuf + "█")
	} else if m.filter.Search != "" {
		b.WriteString("/" + m.filter.Search)
	}
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: "+m.err.Error()) + "\n")
	}

	for i, iss := range m.issues {
		row := fmt.Sprintf("  %-10s  %-12s  %-8s  %s", iss.Key, iss.Status, iss.Type, iss.Summary)
		if i == m.cursor {
			b.WriteString(lipgloss.NewStyle().Bold(true).Reverse(true).Render(row))
		} else {
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	// Status bar.
	mode := "NORMAL"
	switch m.mode {
	case ModeInsert:
		mode = "INSERT"
	case ModeCommand:
		mode = "COMMAND"
	}
	b.WriteString(fmt.Sprintf("\n%d/%d • %s", len(m.issues), m.total, mode))
	if m.mode == ModeCommand {
		b.WriteString("  :" + m.cmdBuf + "█")
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// Test introspection helpers
// -----------------------------------------------------------------------------

func (m Model) Mode() Mode               { return m.mode }
func (m Model) Cursor() int              { return m.cursor }
func (m Model) Issues() []model.Issue    { return m.issues }
func (m Model) SearchBuffer() string     { return m.searchBuf }
func (m Model) CommandBuffer() string    { return m.cmdBuf }
func (m Model) Filter() model.Filter     { return m.filter }
func (m Model) Pending() string          { return m.pending }
func (m Model) ChipFocus() Chip          { return m.chipFocus }
func (m Model) Total() int               { return m.total }

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func keysTrue(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}

func summariseSelected(m map[string]bool) string {
	vs := keysTrue(m)
	if len(vs) == 0 {
		return "All"
	}
	sort.Strings(vs)
	if len(vs) > 2 {
		return fmt.Sprintf("%s, +%d", vs[0], len(vs)-1)
	}
	return strings.Join(vs, ", ")
}

func assigneeLabel(a model.AssigneeFilter) string {
	switch a.Kind {
	case model.AssigneeKindMe:
		return "Me"
	case model.AssigneeKindUnassigned:
		return "Unassigned"
	case model.AssigneeKindAccount:
		if a.AccountID == "" {
			return "All"
		}
		return a.AccountID
	default:
		return "All"
	}
}

var chipStyle = lipgloss.NewStyle().Padding(0, 1)
var chipFocusedStyle = chipStyle.Copy().Bold(true).Reverse(true)

func renderChip(hotkey, label, value string, focused bool) string {
	text := fmt.Sprintf("[%s]%s: %s", hotkey, label, value)
	if focused {
		return chipFocusedStyle.Render(text)
	}
	return chipStyle.Render(text)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func halfPage(h int) int {
	if h <= 0 {
		return 10
	}
	return (h - 6) / 2
}

func pageSize(h int) int {
	if h <= 0 {
		return 20
	}
	return h - 6
}

func looksLikeIssueKey(s string) bool {
	// PROJECT-NUMBER, case-insensitive.
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "" {
		return false
	}
	for _, r := range parts[0] {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	if parts[1] == "" {
		return false
	}
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

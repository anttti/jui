// Package list is the Bubble Tea model for the issue list view.
package list

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/tui/style"
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
	ChipSort
	ChipColumns
	ChipPresetSave
)

// Clipboard abstracts OS clipboard access for testability.
type Clipboard interface{ Copy(string) error }

// Option tweaks a Model.
type Option func(*Model)

// WithClipboard injects a clipboard.
func WithClipboard(c Clipboard) Option { return func(m *Model) { m.clip = c } }

// WithInitialState seeds the model with a previously persisted snapshot.
// Unset fields in the snapshot use the model's defaults.
func WithInitialState(cols []ColumnID, sort []SortKey, presets map[int][]ColumnID, activePreset int, filter model.Filter) Option {
	return func(m *Model) {
		if len(cols) > 0 {
			m.cols = cols
		}
		m.sortKeys = sort
		if presets != nil {
			m.presets = presets
		}
		m.activePreset = activePreset
		m.filter = filter
	}
}

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
	typeValues     []string
	typeIdx        int
	typeSelected   map[string]bool
	statusValues   []string
	statusIdx      int
	statusSelected map[string]bool
	assigneeValues []model.User
	assigneeIdx    int

	// sort state + chip
	sortKeys []SortKey
	sortIdx  int

	// column configuration + presets
	cols         []ColumnID
	colsIdx      int
	colsVisible  map[ColumnID]bool // used while editing in ChipColumns
	colsOrder    []ColumnID        // working order while editing in ChipColumns
	presets      map[int][]ColumnID
	activePreset int

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
		cols:           append([]ColumnID(nil), DefaultColumns...),
		presets:        map[int][]ColumnID{},
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
		applySort(m.issues, m.sortKeys)
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
	case 'o', 'w':
		if m.cursor < len(m.issues) {
			url := m.issues[m.cursor].URL
			return m, func() tea.Msg { return OpenURLMsg{URL: url} }
		}
	case 'q':
		return m, tea.Quit
	case 'z':
		return m.focusChip(ChipSort)
	case 'c':
		return m.focusChip(ChipColumns)
	case 'p':
		m.chipFocus = ChipPresetSave
		return m, nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		slot := int(msg.Runes[0] - '0')
		return m.recallPreset(slot)
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
		m.typeSelected = map[string]bool{}
		for _, t := range m.filter.Types {
			m.typeSelected[t] = true
		}
	case ChipStatus:
		vs, _ := m.reader.DistinctStatuses(ctx)
		m.statusValues = vs
		m.statusIdx = 0
		m.statusSelected = map[string]bool{}
		for _, s := range m.filter.Statuses {
			m.statusSelected[s] = true
		}
	case ChipAssignee:
		us, _ := m.reader.Assignees(ctx)
		// Prepend a virtual "All" row at index 0 so the user can clear
		// the filter from within the dropdown. Empty AccountID is the
		// sentinel.
		m.assigneeValues = append([]model.User{{DisplayName: "All"}}, us...)
		m.assigneeIdx = 0
	case ChipSort:
		m.sortIdx = 0
	case ChipColumns:
		// Prime a working copy so Esc can cancel without mutating the
		// live order.
		m.colsOrder = append([]ColumnID(nil), m.cols...)
		// Hidden columns live past the visible ones in the working order.
		for _, id := range AllColumns {
			if !containsCol(m.colsOrder, id) {
				m.colsOrder = append(m.colsOrder, id)
			}
		}
		m.colsVisible = map[ColumnID]bool{}
		for _, id := range m.cols {
			m.colsVisible[id] = true
		}
		m.colsIdx = 0
	}
	return m, nil
}

func (m Model) handleChip(msg tea.KeyMsg) (Model, tea.Cmd) {
	// The preset-save popup is modal: only a digit or Esc is meaningful.
	if m.chipFocus == ChipPresetSave {
		if msg.Type == tea.KeyEsc {
			m.chipFocus = ChipNone
			return m, nil
		}
		if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
			slot := int(msg.Runes[0] - '0')
			m.presets[slot] = append([]ColumnID(nil), m.cols...)
			m.activePreset = slot
			m.chipFocus = ChipNone
			}
		return m, nil
	}
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
				u := m.assigneeValues[m.assigneeIdx]
				if u.AccountID == "" {
					m.filter.Assignee = model.AssigneeAll()
				} else {
					m.filter.Assignee = model.AssigneeAccount(u.AccountID)
				}
			} else {
				m.filter.Assignee = model.AssigneeAll()
			}
		case ChipSort:
			// Sort has no commit step — changes are applied as you toggle.
		case ChipColumns:
			var next []ColumnID
			for _, id := range m.colsOrder {
				if m.colsVisible[id] {
					next = append(next, id)
				}
			}
			if len(next) > 0 {
				m.cols = next
				m.activePreset = 0
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
		case ChipSort:
			if m.sortIdx < len(SortableColumns) {
				col := SortableColumns[m.sortIdx]
				m.sortKeys = toggleSortKey(m.sortKeys, col)
				applySort(m.issues, m.sortKeys)
					}
		case ChipColumns:
			if m.colsIdx < len(m.colsOrder) {
				id := m.colsOrder[m.colsIdx]
				m.colsVisible[id] = !m.colsVisible[id]
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
			case ChipSort:
				m.sortIdx = clamp(m.sortIdx+1, 0, len(SortableColumns)-1)
			case ChipColumns:
				m.colsIdx = clamp(m.colsIdx+1, 0, len(m.colsOrder)-1)
			}
		case 'k':
			switch m.chipFocus {
			case ChipType:
				m.typeIdx = clamp(m.typeIdx-1, 0, len(m.typeValues)-1)
			case ChipStatus:
				m.statusIdx = clamp(m.statusIdx-1, 0, len(m.statusValues)-1)
			case ChipAssignee:
				m.assigneeIdx = clamp(m.assigneeIdx-1, 0, len(m.assigneeValues)-1)
			case ChipSort:
				m.sortIdx = clamp(m.sortIdx-1, 0, len(SortableColumns)-1)
			case ChipColumns:
				m.colsIdx = clamp(m.colsIdx-1, 0, len(m.colsOrder)-1)
			}
		case 'J':
			// Move the focused column down (only in ChipColumns).
			if m.chipFocus == ChipColumns && m.colsIdx+1 < len(m.colsOrder) {
				m.colsOrder[m.colsIdx], m.colsOrder[m.colsIdx+1] = m.colsOrder[m.colsIdx+1], m.colsOrder[m.colsIdx]
				m.colsIdx++
			}
		case 'K':
			// Move the focused column up (only in ChipColumns).
			if m.chipFocus == ChipColumns && m.colsIdx-1 >= 0 {
				m.colsOrder[m.colsIdx], m.colsOrder[m.colsIdx-1] = m.colsOrder[m.colsIdx-1], m.colsOrder[m.colsIdx]
				m.colsIdx--
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

// recallPreset switches the active column set to the preset in the given
// slot. Slots with no preset are silently ignored.
func (m Model) recallPreset(slot int) (Model, tea.Cmd) {
	cols, ok := m.presets[slot]
	if !ok || len(cols) == 0 {
		return m, nil
	}
	m.cols = append([]ColumnID(nil), cols...)
	m.activePreset = slot
	return m, nil
}

func containsCol(list []ColumnID, id ColumnID) bool {
	for _, c := range list {
		if c == id {
			return true
		}
	}
	return false
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

// View renders the list as a toolbar, optional filter dropdown, table of
// issues, and status-bar footer.
func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 120
	}

	var b strings.Builder
	b.WriteString(m.renderToolbar(w))
	b.WriteString("\n")

	if m.chipFocus != ChipNone {
		dd := strings.TrimRight(m.renderDropdown(), "\n")
		b.WriteString(style.Panel.Render(dd))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(style.Error.Render("error: "+m.err.Error()) + "\n")
	}

	b.WriteString(renderHeaderLine(m.cols, w, m.sortKeys))
	b.WriteString("\n")

	widths := computeColumnWidths(m.cols, w)
	for i, iss := range m.issues {
		b.WriteString(renderIssueRow(m.cols, widths, iss, w, i == m.cursor))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatusBar(w))
	return b.String()
}

func (m Model) renderToolbar(w int) string {
	tabs := []string{
		renderChip("t", "type", summariseSelected(m.typeSelected), m.chipFocus == ChipType),
		renderChip("s", "status", summariseSelected(m.statusSelected), m.chipFocus == ChipStatus),
		renderChip("a", "assignee", assigneeLabel(m.filter.Assignee), m.chipFocus == ChipAssignee),
		renderChip("z", "sort", summariseSort(m.sortKeys), m.chipFocus == ChipSort),
		renderChip("c", "cols", summariseCols(m.cols, m.activePreset), m.chipFocus == ChipColumns),
	}
	tabsStr := strings.Join(tabs, style.TabSeparator)

	var right string
	if m.mode == ModeInsert {
		right = style.MutedText.Render("/") + m.searchBuf + "█"
	} else if m.filter.Search != "" {
		right = style.MutedText.Render("/" + m.filter.Search)
	}
	if right == "" {
		return tabsStr
	}
	tw := lipgloss.Width(tabsStr)
	rw := lipgloss.Width(right)
	if tw+rw+1 >= w {
		return tabsStr + " " + right
	}
	return tabsStr + strings.Repeat(" ", w-tw-rw) + right
}


func (m Model) renderStatusBar(w int) string {
	mode := "NORMAL"
	switch m.mode {
	case ModeInsert:
		mode = "INSERT"
	case ModeCommand:
		mode = "COMMAND"
	}
	left := style.ModePill(mode)
	count := fmt.Sprintf("  %d/%d", len(m.issues), m.total)

	var hint string
	switch {
	case m.mode == ModeCommand:
		hint = ":" + m.cmdBuf + "█"
	case m.chipFocus == ChipAssignee:
		hint = "j/k move · enter select · esc cancel"
	case m.chipFocus != ChipNone:
		hint = "j/k move · space toggle · enter apply · esc cancel"
	default:
		hint = "j/k move · / search · : cmd · enter open"
	}
	right := style.MutedText.Render(hint) + "  "

	lw := lipgloss.Width(left)
	cw := lipgloss.Width(count)
	rw := lipgloss.Width(right)
	pad := w - lw - cw - rw
	if pad < 1 {
		pad = 1
	}
	inside := left + count + strings.Repeat(" ", pad) + right
	return style.StatusBar.Copy().Width(w).MaxWidth(w).Render(inside)
}

func (m Model) renderDropdown() string {
	var b strings.Builder
	switch m.chipFocus {
	case ChipType:
		for i, v := range m.typeValues {
			b.WriteString(renderDropdownRow(i == m.typeIdx, true, m.typeSelected[v], v))
		}
	case ChipStatus:
		for i, v := range m.statusValues {
			b.WriteString(renderDropdownRow(i == m.statusIdx, true, m.statusSelected[v], v))
		}
	case ChipAssignee:
		for i, u := range m.assigneeValues {
			label := u.DisplayName
			if label == "" {
				label = u.AccountID
			}
			b.WriteString(renderDropdownRow(i == m.assigneeIdx, false, false, label))
		}
	case ChipSort:
		sortDir := map[ColumnID]string{}
		sortPos := map[ColumnID]int{}
		for i, k := range m.sortKeys {
			if k.Desc {
				sortDir[k.Column] = "↓"
			} else {
				sortDir[k.Column] = "↑"
			}
			sortPos[k.Column] = i + 1
		}
		for i, id := range SortableColumns {
			label := columnLabel(id)
			if dir, ok := sortDir[id]; ok {
				label = fmt.Sprintf("%s %s (%d)", label, dir, sortPos[id])
			}
			b.WriteString(renderDropdownRow(i == m.sortIdx, false, false, label))
		}
	case ChipColumns:
		for i, id := range m.colsOrder {
			label := columnLabel(id) + " — " + string(id)
			b.WriteString(renderDropdownRow(i == m.colsIdx, true, m.colsVisible[id], label))
		}
	case ChipPresetSave:
		b.WriteString(style.MutedText.Render("press 1-9 to save current columns to that preset slot:") + "\n")
		for i := 1; i <= 9; i++ {
			marker := "  "
			note := "(empty)"
			if cols, ok := m.presets[i]; ok && len(cols) > 0 {
				marker = "• "
				note = fmt.Sprintf("(%d cols)", len(cols))
			}
			b.WriteString(fmt.Sprintf("%s%d %s\n", marker, i, note))
		}
	}
	return b.String()
}

func renderDropdownRow(cursor, checkbox, checked bool, label string) string {
	var prefix string
	if cursor {
		prefix = "▸ "
	} else {
		prefix = "  "
	}
	var check string
	if checkbox {
		if checked {
			check = "[x] "
		} else {
			check = "[ ] "
		}
	} else {
		check = "• "
	}
	line := prefix + check + label
	if cursor {
		line = lipgloss.NewStyle().Foreground(style.Primary).Bold(true).Render(line)
	}
	return line + "\n"
}

// -----------------------------------------------------------------------------
// Test introspection helpers
// -----------------------------------------------------------------------------

func (m Model) Mode() Mode                    { return m.mode }
func (m Model) Cursor() int                   { return m.cursor }
func (m Model) Issues() []model.Issue         { return m.issues }
func (m Model) SearchBuffer() string          { return m.searchBuf }
func (m Model) CommandBuffer() string         { return m.cmdBuf }
func (m Model) Filter() model.Filter          { return m.filter }
func (m Model) Pending() string               { return m.pending }
func (m Model) ChipFocus() Chip               { return m.chipFocus }
func (m Model) Total() int                    { return m.total }
func (m Model) Sort() []SortKey               { return m.sortKeys }
func (m Model) Columns() []ColumnID           { return m.cols }
func (m Model) Presets() map[int][]ColumnID   { return m.presets }
func (m Model) ActivePreset() int             { return m.activePreset }

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

func summariseSort(keys []SortKey) string {
	if len(keys) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		arrow := "↑"
		if k.Desc {
			arrow = "↓"
		}
		parts = append(parts, columnLabel(k.Column)+arrow)
	}
	return strings.Join(parts, ", ")
}

func summariseCols(cols []ColumnID, activePreset int) string {
	if activePreset > 0 {
		return fmt.Sprintf("preset %d (%d)", activePreset, len(cols))
	}
	return fmt.Sprintf("%d", len(cols))
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

func renderChip(hotkey, label, value string, focused bool) string {
	text := fmt.Sprintf("[%s] %s: %s", hotkey, label, value)
	if focused {
		return style.TabFocused.Render(text)
	}
	return style.Tab.Render(text)
}

// relTime returns a compact human-readable age like "5m", "2h", "3d", "1w",
// "2y". Zero times render as "—".
func relTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d/(7*24*time.Hour)))
	default:
		return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
	}
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

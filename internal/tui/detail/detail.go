// Package detail is the Bubble Tea model for the single-issue detail view.
package detail

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
)

// Clipboard is the OS clipboard abstraction.
type Clipboard interface{ Copy(string) error }

// Option tweaks a Model.
type Option func(*Model)

// WithClipboard injects a clipboard.
func WithClipboard(c Clipboard) Option { return func(m *Model) { m.clip = c } }

// BackMsg is emitted when the user pops back to the list.
type BackMsg struct{}

// OpenURLMsg is emitted when the user presses `o`.
type OpenURLMsg struct{ URL string }

// commentsLoadedMsg is the result of the comments-fetch Cmd.
type commentsLoadedMsg struct {
	key      string
	comments []model.Comment
	err      error
}

// Model is the detail view.
type Model struct {
	reader store.Reader
	clip   Clipboard

	issues []model.Issue
	idx    int

	comments []model.Comment
	scroll   int

	pending string
	err     error

	width, height int
}

// New constructs a detail Model. issues is the list of siblings (the list
// view's visible rows) so ]/[ can navigate without going back to the list.
func New(r store.Reader, issues []model.Issue, idx int, opts ...Option) Model {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(issues) {
		idx = len(issues) - 1
	}
	m := Model{reader: r, issues: issues, idx: idx}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// Init fires the comments-load for the current issue.
func (m Model) Init() tea.Cmd { return m.loadCommentsCmd(m.Current().Key) }

// Update advances state.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commentsLoadedMsg:
		if msg.key == m.Current().Key {
			m.comments = msg.comments
			m.err = msg.err
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

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Multi-key sequences (gg).
	if m.pending != "" {
		combined := m.pending + msg.String()
		m.pending = ""
		if combined == "gg" {
			m.scroll = 0
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m, func() tea.Msg { return BackMsg{} }
	case tea.KeyUp:
		m.scroll = clamp(m.scroll-1, 0, m.MaxScroll())
		return m, nil
	case tea.KeyDown:
		m.scroll = clamp(m.scroll+1, 0, m.MaxScroll())
		return m, nil
	case tea.KeyCtrlD:
		m.scroll = clamp(m.scroll+halfPage(m.height), 0, m.MaxScroll())
		return m, nil
	case tea.KeyCtrlU:
		m.scroll = clamp(m.scroll-halfPage(m.height), 0, m.MaxScroll())
		return m, nil
	}
	if len(msg.Runes) != 1 {
		return m, nil
	}
	switch msg.Runes[0] {
	case 'h', 'q':
		return m, func() tea.Msg { return BackMsg{} }
	case 'j':
		m.scroll = clamp(m.scroll+1, 0, m.MaxScroll())
	case 'k':
		m.scroll = clamp(m.scroll-1, 0, m.MaxScroll())
	case 'g':
		m.pending = "g"
	case 'G':
		m.scroll = m.MaxScroll()
	case ']':
		if m.idx+1 < len(m.issues) {
			m.idx++
			m.scroll = 0
			m.comments = nil
			return m, m.loadCommentsCmd(m.Current().Key)
		}
	case '[':
		if m.idx-1 >= 0 {
			m.idx--
			m.scroll = 0
			m.comments = nil
			return m, m.loadCommentsCmd(m.Current().Key)
		}
	case 'o':
		url := m.Current().URL
		return m, func() tea.Msg { return OpenURLMsg{URL: url} }
	case 'y':
		if m.clip != nil {
			_ = m.clip.Copy(m.Current().Key)
		}
	}
	return m, nil
}

// loadCommentsCmd fires a Cmd that fetches comments for the given key.
func (m Model) loadCommentsCmd(key string) tea.Cmd {
	r := m.reader
	return func() tea.Msg {
		cs, err := r.Comments(context.Background(), key)
		return commentsLoadedMsg{key: key, comments: cs, err: err}
	}
}

// View renders the detail view. Simple for now; golden rendering comes later.
func (m Model) View() string {
	iss := m.Current()
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", iss.Key, iss.Summary)
	fmt.Fprintf(&b, "Status: %s   Type: %s   Priority: %s\n", iss.Status, iss.Type, iss.Priority)
	if iss.Assignee != nil {
		fmt.Fprintf(&b, "Assignee: %s   ", iss.Assignee.DisplayName)
	}
	if iss.Reporter != nil {
		fmt.Fprintf(&b, "Reporter: %s", iss.Reporter.DisplayName)
	}
	b.WriteString("\n")
	if iss.DueDate != nil {
		fmt.Fprintf(&b, "Due: %s\n", iss.DueDate.Format("2006-01-02"))
	}
	b.WriteString("\n# Description\n\n")
	b.WriteString(iss.Description)
	b.WriteString("\n\n")

	if len(m.comments) > 0 {
		fmt.Fprintf(&b, "# Comments (%d)\n\n", len(m.comments))
		for _, c := range m.comments {
			name := ""
			if c.Author != nil {
				name = c.Author.DisplayName
			}
			fmt.Fprintf(&b, "@%s · %s\n%s\n---\n", name, c.Created.Format("2006-01-02 15:04"), c.Body)
		}
	}

	// Return sliced by scroll.
	lines := strings.Split(b.String(), "\n")
	start := clamp(m.scroll, 0, len(lines))
	end := clamp(start+pageSize(m.height), 0, len(lines))
	return strings.Join(lines[start:end], "\n")
}

// -----------------------------------------------------------------------------
// Introspection + sizing
// -----------------------------------------------------------------------------

// Current returns the currently displayed issue.
func (m Model) Current() model.Issue {
	if m.idx < 0 || m.idx >= len(m.issues) {
		return model.Issue{}
	}
	return m.issues[m.idx]
}

// Comments returns the loaded comments.
func (m Model) Comments() []model.Comment { return m.comments }

// Scroll returns the current scroll offset in lines.
func (m Model) Scroll() int { return m.scroll }

// MaxScroll returns the max valid scroll offset.
func (m Model) MaxScroll() int {
	total := m.contentLines()
	max := total - pageSize(m.height)
	if max < 0 {
		return 0
	}
	return max
}

// Pending returns the pending-key buffer (gg multi-key).
func (m Model) Pending() string { return m.pending }

// SetSize lets tests configure the viewport without a window-size message.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m Model) contentLines() int {
	// Count lines in the fully-rendered content (pre-slice). Mirror View.
	iss := m.Current()
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", iss.Key, iss.Summary)
	fmt.Fprintf(&b, "Status: %s   Type: %s   Priority: %s\n", iss.Status, iss.Type, iss.Priority)
	b.WriteString("\n")
	if iss.DueDate != nil {
		b.WriteString("due\n")
	}
	b.WriteString("\n# Description\n\n")
	b.WriteString(iss.Description)
	b.WriteString("\n\n")
	if len(m.comments) > 0 {
		b.WriteString("# Comments\n\n")
		for range m.comments {
			b.WriteString("\n\n---\n")
		}
	}
	return strings.Count(b.String(), "\n")
}

// -----------------------------------------------------------------------------
// utils
// -----------------------------------------------------------------------------

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

func pageSize(h int) int {
	if h <= 0 {
		return 20
	}
	n := h - 4
	if n < 1 {
		return 1
	}
	return n
}

func halfPage(h int) int {
	n := pageSize(h) / 2
	if n < 1 {
		return 1
	}
	return n
}

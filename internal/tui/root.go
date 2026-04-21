// Package tui is the Bubble Tea root that routes messages between the
// list and detail views.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/sync"
	"github.com/anttti/j/internal/tui/detail"
	"github.com/anttti/j/internal/tui/list"
)

// View enumerates the root's top-level screens.
type View int

const (
	ViewList View = iota
	ViewDetail
)

// Clipboard abstracts OS clipboard access for testability.
type Clipboard interface{ Copy(string) error }

// Opener abstracts launching URLs in the browser.
type Opener interface{ Open(url string) error }

// Deps is the constructor bundle.
type Deps struct {
	Store     store.Reader
	Fetcher   sync.IssueFetcher
	Clipboard Clipboard
	Opener    Opener
}

// Model is the root tea.Model.
type Model struct {
	reader  store.Reader
	fetcher sync.IssueFetcher
	clip    Clipboard
	opener  Opener

	list    list.Model
	detail  detail.Model
	view    View

	err error
}

// New constructs the root Model.
func New(deps Deps) Model {
	var listOpts []list.Option
	if deps.Clipboard != nil {
		listOpts = append(listOpts, list.WithClipboard(clipAdapter{deps.Clipboard}))
	}
	return Model{
		reader:  deps.Store,
		fetcher: deps.Fetcher,
		clip:    deps.Clipboard,
		opener:  deps.Opener,
		list:    list.New(deps.Store, listOpts...),
		view:    ViewList,
	}
}

// clipAdapter bridges the root's Clipboard interface to list.Clipboard.
type clipAdapter struct{ c Clipboard }

func (a clipAdapter) Copy(s string) error { return a.c.Copy(s) }

// Init returns the initial command from the list view.
func (m Model) Init() tea.Cmd { return m.list.Init() }

// Update routes messages through the active view and handles cross-view
// events.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// Global keys first.
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case list.OpenIssueMsg:
		return m.openIssue(msg.Key)

	case fetchDoneMsg:
		return m.onFetchDone(msg)

	case detail.BackMsg:
		m.view = ViewList
		m.detail = detail.Model{}
		return m, nil

	case list.OpenURLMsg:
		if m.opener != nil {
			_ = m.opener.Open(msg.URL)
		}
		return m, nil

	case detail.OpenURLMsg:
		if m.opener != nil {
			_ = m.opener.Open(msg.URL)
		}
		return m, nil
	}

	// Route to the active view.
	switch m.view {
	case ViewList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	case ViewDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View renders whichever screen is active.
func (m Model) View() string {
	switch m.view {
	case ViewDetail:
		return m.detail.View()
	default:
		return m.list.View()
	}
}

// -----------------------------------------------------------------------------
// Cross-view plumbing
// -----------------------------------------------------------------------------

// fetchDoneMsg is the result of an on-demand FetchOne.
type fetchDoneMsg struct {
	key string
	err error
}

func (m Model) openIssue(key string) (Model, tea.Cmd) {
	iss, err := m.reader.Get(context.Background(), key)
	if err != nil {
		m.err = err
		return m, nil
	}
	if iss != nil {
		return m.pushDetail(*iss)
	}
	// Not in store — fetch, then open.
	if m.fetcher == nil {
		return m, nil
	}
	f := m.fetcher
	return m, func() tea.Msg {
		err := f.FetchOne(context.Background(), key)
		return fetchDoneMsg{key: key, err: err}
	}
}

func (m Model) onFetchDone(msg fetchDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	iss, err := m.reader.Get(context.Background(), msg.key)
	if err != nil {
		m.err = err
		return m, nil
	}
	if iss == nil {
		return m, nil
	}
	return m.pushDetail(*iss)
}

func (m Model) pushDetail(iss model.Issue) (Model, tea.Cmd) {
	// Use the list's current rows as the sibling set if the issue is in
	// them; otherwise make a single-element sibling list.
	siblings := m.list.Issues()
	idx := -1
	for i, s := range siblings {
		if s.Key == iss.Key {
			idx = i
			break
		}
	}
	if idx < 0 {
		siblings = []model.Issue{iss}
		idx = 0
	}
	var detailOpts []detail.Option
	if m.clip != nil {
		detailOpts = append(detailOpts, detail.WithClipboard(detailClipAdapter{m.clip}))
	}
	m.detail = detail.New(m.reader, siblings, idx, detailOpts...)
	m.view = ViewDetail
	return m, m.detail.Init()
}

// detailClipAdapter mirrors clipAdapter for the detail package's interface.
type detailClipAdapter struct{ c Clipboard }

func (a detailClipAdapter) Copy(s string) error { return a.c.Copy(s) }

// -----------------------------------------------------------------------------
// Introspection helpers
// -----------------------------------------------------------------------------

// CurrentView reports which screen is active.
func (m Model) CurrentView() View { return m.view }

// DetailKey returns the currently displayed issue key (empty when not on
// the detail view).
func (m Model) DetailKey() string {
	if m.view != ViewDetail {
		return ""
	}
	return m.detail.Current().Key
}

// Err returns the last root-level error (fetch failure, etc.).
func (m Model) Err() error { return m.err }

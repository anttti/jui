// Package tui is the Bubble Tea root that routes messages between the
// list and detail views.
package tui

import (
	"context"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anttti/j/internal/model"
	"github.com/anttti/j/internal/store"
	"github.com/anttti/j/internal/sync"
	"github.com/anttti/j/internal/tui/appstate"
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
	// StatePath, when non-empty, enables load-on-init / save-on-change of
	// persistent UI state (filters, sort, column presets, last view).
	StatePath string
}

// Model is the root tea.Model.
type Model struct {
	reader    store.Reader
	fetcher   sync.IssueFetcher
	clip      Clipboard
	opener    Opener
	statePath string

	list   list.Model
	detail detail.Model
	view   View

	// pendingOpen, if non-empty, is the key to re-open on startup after the
	// list's initial load completes.
	pendingOpen string

	err error
}

// New constructs the root Model.
func New(deps Deps) Model {
	var listOpts []list.Option
	if deps.Clipboard != nil {
		listOpts = append(listOpts, list.WithClipboard(clipAdapter{deps.Clipboard}))
	}

	// Pull previously persisted state, if any.
	st, _ := appstate.Load(deps.StatePath)
	cols := decodeColumns(st.Columns)
	sortKeys := decodeSort(st.Sort)
	presets := decodePresets(st.Presets)
	filter := decodeFilter(st.Filter)

	listOpts = append(listOpts,
		list.WithInitialState(cols, sortKeys, presets, st.ActivePreset, filter),
	)
	return Model{
		reader:      deps.Store,
		fetcher:     deps.Fetcher,
		clip:        deps.Clipboard,
		opener:      deps.Opener,
		statePath:   deps.StatePath,
		view:        ViewList,
		pendingOpen: st.SelectedKey,
		list:        list.New(deps.Store, listOpts...),
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
		m.save()
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case list.OpenIssueMsg:
		next, cmd := m.openIssue(msg.Key)
		next.save()
		return next, cmd

	case fetchDoneMsg:
		next, cmd := m.onFetchDone(msg)
		next.save()
		return next, cmd

	case detail.BackMsg:
		m.view = ViewList
		m.detail = detail.Model{}
		m.save()
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

	var cmd tea.Cmd
	switch m.view {
	case ViewList:
		m.list, cmd = m.list.Update(msg)
	case ViewDetail:
		m.detail, cmd = m.detail.Update(msg)
	}

	// Re-open the last-viewed issue once the list has loaded. This is the
	// one place we consume pendingOpen; clear it to avoid looping.
	if m.pendingOpen != "" && m.view == ViewList {
		if _, ok := msg.(tea.KeyMsg); !ok {
			// Try to reopen only after data lands; `loadedMsg` is internal
			// to the list package, so we just opportunistically try on any
			// non-key message.
			key := m.pendingOpen
			m.pendingOpen = ""
			if len(m.list.Issues()) > 0 || key != "" {
				reopen, rcmd := m.openIssue(key)
				reopen.save()
				return reopen, tea.Batch(cmd, rcmd)
			}
		}
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		m.save()
	}
	return m, cmd
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

// -----------------------------------------------------------------------------
// Persistence
// -----------------------------------------------------------------------------

func (m Model) save() {
	if m.statePath == "" {
		return
	}
	_ = appstate.Save(m.statePath, m.snapshot())
}

func (m Model) snapshot() appstate.State {
	var selected string
	switch m.view {
	case ViewDetail:
		selected = m.detail.Current().Key
	case ViewList:
		if iss := m.list.Issues(); len(iss) > 0 {
			c := m.list.Cursor()
			if c >= 0 && c < len(iss) {
				selected = iss[c].Key
			}
		}
	}
	view := "list"
	if m.view == ViewDetail {
		view = "detail"
	}
	return appstate.State{
		View:         view,
		SelectedKey:  selected,
		Filter:       encodeFilter(m.list.Filter()),
		Sort:         encodeSort(m.list.Sort()),
		Columns:      encodeColumns(m.list.Columns()),
		ActivePreset: m.list.ActivePreset(),
		Presets:      encodePresets(m.list.Presets()),
	}
}

func encodeFilter(f model.Filter) appstate.Filter {
	out := appstate.Filter{
		Types:    append([]string(nil), f.Types...),
		Statuses: append([]string(nil), f.Statuses...),
		Search:   f.Search,
	}
	switch f.Assignee.Kind {
	case model.AssigneeKindMe:
		out.Assignee = appstate.Assignee{Kind: "me"}
	case model.AssigneeKindUnassigned:
		out.Assignee = appstate.Assignee{Kind: "unassigned"}
	case model.AssigneeKindAccount:
		out.Assignee = appstate.Assignee{Kind: "account", AccountID: f.Assignee.AccountID}
	default:
		out.Assignee = appstate.Assignee{Kind: "all"}
	}
	return out
}

func decodeFilter(f appstate.Filter) model.Filter {
	out := model.Filter{
		Types:    append([]string(nil), f.Types...),
		Statuses: append([]string(nil), f.Statuses...),
		Search:   f.Search,
	}
	switch f.Assignee.Kind {
	case "me":
		out.Assignee = model.AssigneeMe()
	case "unassigned":
		out.Assignee = model.AssigneeUnassigned()
	case "account":
		out.Assignee = model.AssigneeAccount(f.Assignee.AccountID)
	default:
		out.Assignee = model.AssigneeAll()
	}
	return out
}

func encodeSort(keys []list.SortKey) []appstate.SortKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]appstate.SortKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, appstate.SortKey{Column: string(k.Column), Desc: k.Desc})
	}
	return out
}

func decodeSort(keys []appstate.SortKey) []list.SortKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]list.SortKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, list.SortKey{Column: list.ColumnID(k.Column), Desc: k.Desc})
	}
	return out
}

func encodeColumns(cols []list.ColumnID) []string {
	if len(cols) == 0 {
		return nil
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, string(c))
	}
	return out
}

func decodeColumns(cols []string) []list.ColumnID {
	if len(cols) == 0 {
		return nil
	}
	out := make([]list.ColumnID, 0, len(cols))
	for _, c := range cols {
		out = append(out, list.ColumnID(c))
	}
	return out
}

func encodePresets(p map[int][]list.ColumnID) map[string][]string {
	if len(p) == 0 {
		return nil
	}
	out := map[string][]string{}
	for slot, cols := range p {
		out[strconv.Itoa(slot)] = encodeColumns(cols)
	}
	return out
}

func decodePresets(p map[string][]string) map[int][]list.ColumnID {
	if len(p) == 0 {
		return nil
	}
	out := map[int][]list.ColumnID{}
	for slot, cols := range p {
		n, err := strconv.Atoi(slot)
		if err != nil {
			continue
		}
		out[n] = decodeColumns(cols)
	}
	return out
}

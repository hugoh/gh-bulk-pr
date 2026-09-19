package ui

import (
	"sync/atomic"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

const (
	keyEnter = "enter"
	keyEsc   = "esc"
)

// Update handles bubbletea messages: window resizes, search/action results,
// and key presses, routed by the current screen.
//
//nolint:ireturn // Update must satisfy the tea.Model interface
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg), nil
	case searchDoneMsg:
		return m.handleSearchDone(msg), nil
	case actionDoneMsg:
		m.results = msg.results
		m.screen = screenResults

		return m, nil

	case actionProgressMsg:
		return m.handleActionProgress()

	case spinner.TickMsg:
		spinnerActive := m.loading || (m.screen == screenResults && m.results == nil)
		if !spinnerActive {
			return m, nil
		}

		var cmd tea.Cmd

		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

const (
	previewHeightMargin = 6
	minListHeight       = 5
	previewListFraction = 4 // preview open: list gets at most 1/4 of the available height
)

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.table.SetWidth(m.width)
	m.table.SetColumns(columnsForWidth(m.width))
	m = m.syncTableHeight()

	return m
}

// syncTableHeight sizes the PR table for the current window and preview
// state: full height with no preview open, otherwise at least minListHeight
// lines and at most a quarter of the available height, leaving the rest for
// the preview panel below it.
func (m Model) syncTableHeight() Model {
	available := m.height - previewHeightMargin
	if !m.previewOpen {
		m.table.SetHeight(available)

		return m
	}

	m.table.SetHeight(max(available/previewListFraction, minListHeight))

	return m
}

// handleActionProgress re-renders the progress bar and keeps polling until
// actionDoneMsg replaces the screen; it stops rescheduling once results land
// or the model has moved off the results screen.
func (m Model) handleActionProgress() (Model, tea.Cmd) {
	if m.screen != screenResults || m.results != nil {
		return m, nil
	}

	return m, pollActionProgress()
}

func (m Model) handleSearchDone(msg searchDoneMsg) Model {
	m.loading = false
	m.err = msg.err

	if msg.err == nil {
		m.prs = msg.prs
		m.selected = map[prKey]bool{}
		m = m.refreshRows()
	}

	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.screen {
	case screenList:
		return m.handleListKey(msg)
	case screenFilter:
		return m.handleFilterKey(msg)
	case screenActionInput:
		return m.handleActionInputKey(msg)
	case screenConfirm:
		return m.handleConfirmKey(msg)
	case screenResults:
		return m.handleResultsKey(msg)
	}

	return m, nil
}

func (m Model) handleResultsKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.String() == keyEnter || msg.String() == keyEsc {
		m.screen = screenList

		return m.reload()
	}

	return m, nil
}

func (m Model) reload() (Model, tea.Cmd) {
	m.loading = true

	return m, tea.Batch(m.search(m.query), m.spinner.Tick)
}

// runQuery makes query current, selecting the tab it matches (if any),
// records it in the history and reloads the list.
func (m Model) runQuery(query string) (Model, tea.Cmd) {
	m.query = query
	m.tab = tabFor(query)
	_ = m.history.Add(query) // best effort: history must never block a search

	return m.reload()
}

func (m Model) handleListKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		return m.startFilter()
	case keyEsc:
		return m.clearSelectionOrPreview()
	case keyEnter, "p":
		m.previewOpen = !m.previewOpen
		m = m.syncTableHeight()

		return m, nil
	case "x", " ":
		return m.toggleFocusedSelection()
	case "ctrl+a":
		return m.selectAll()
	case "r":
		return m.reload()
	case "T", "l", "c", "m", "1", "2":
		return m.handleCommandKey(msg)
	}

	var cmd tea.Cmd

	m.table, cmd = m.table.Update(msg)

	return m, cmd
}

func (m Model) handleCommandKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "T":
		return m, m.launchEnhance()
	case "1", "2":
		return m.runQuery(tabs()[int(key[0]-'1')].query)
	default:
		return m.startAction(key)
	}
}

func (m Model) startFilter() (Model, tea.Cmd) {
	m.screen = screenFilter
	m.filterInput.SetValue(m.query)
	m.filterInput.Focus()

	return m, nil
}

func (m Model) clearSelectionOrPreview() (Model, tea.Cmd) {
	if m.previewOpen {
		m.previewOpen = false
		m = m.syncTableHeight()

		return m, nil
	}

	m.selected = map[prKey]bool{}
	m = m.refreshRows()

	return m, nil
}

func (m Model) toggleFocusedSelection() (Model, tea.Cmd) {
	if pr, ok := m.focusedPR(); ok {
		if key := keyOf(pr); m.selected[key] {
			delete(m.selected, key)
		} else {
			m.selected[key] = true
		}

		m = m.refreshRows()
	}

	return m, nil
}

func (m Model) selectAll() (Model, tea.Cmd) {
	for _, pr := range m.prs {
		m.selected[keyOf(pr)] = true
	}

	m = m.refreshRows()

	return m, nil
}

func (m Model) startAction(key string) (Model, tea.Cmd) {
	if len(m.selected) == 0 {
		return m, nil
	}

	if needsInput(key) {
		m.actionKey = key
		m.screen = screenActionInput
		m.actionInput.SetValue("")
		m.actionInput.Focus()

		return m, nil
	}

	m.action = actionsForKey(m.client, key, "")
	m.confirm = m.selectedPRs()
	m.screen = screenConfirm

	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		m.filterInput.Blur()
		m.screen = screenList

		return m.runQuery(m.filterInput.Value())
	case keyEsc:
		m.filterInput.Blur()
		m.screen = screenList

		return m, nil
	}

	var cmd tea.Cmd

	m.filterInput, cmd = m.filterInput.Update(msg)

	return m, cmd
}

func (m Model) handleActionInputKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		input := m.actionInput.Value()
		m.actionInput.Blur()
		m.action = actionsForKey(m.client, m.actionKey, input)
		m.confirm = m.selectedPRs()
		m.screen = screenConfirm

		return m, nil
	case keyEsc:
		m.actionInput.Blur()
		m.screen = screenList

		return m, nil
	}

	var cmd tea.Cmd

	m.actionInput, cmd = m.actionInput.Update(msg)

	return m, cmd
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", keyEnter:
		m.screen = screenResults
		m.results = nil
		m.actionDone = new(atomic.Int32)
		m.actionTotal = len(m.confirm)

		return m, tea.Batch(m.runAction(), pollActionProgress(), m.spinner.Tick)
	case "n", keyEsc:
		m.screen = screenList

		return m, nil
	}

	return m, nil
}

// prKey identifies a PR across repos; PR numbers alone collide.
type prKey struct {
	repo   string
	number int
}

func keyOf(pr github.PR) prKey { return prKey{pr.Repo, pr.Number} }

func (m Model) focusedPR() (github.PR, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.prs) {
		return github.PR{}, false
	}

	return m.prs[i], true
}

func (m Model) selectedPRs() []github.PR {
	var out []github.PR

	for _, pr := range m.prs {
		if m.selected[keyOf(pr)] {
			out = append(out, pr)
		}
	}

	return out
}

func (m Model) refreshRows() Model {
	m.table.SetRows(rowsFor(m.prs, m.selected))

	return m
}

func rowsFor(prs []github.PR, selected map[prKey]bool) []table.Row {
	rows := make([]table.Row, len(prs))
	for idx, entry := range prs {
		mark := " "
		if selected[keyOf(entry)] {
			mark = "x"
		}

		rows[idx] = table.Row{
			mark,
			entry.Repo,
			prNumber(entry.Number),
			entry.Title,
			checksSummary(entry),
			mergeLabel(entry.MergeState),
			entry.Author,
		}
	}

	return rows
}

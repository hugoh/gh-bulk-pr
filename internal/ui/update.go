package ui

import (
	"context"
	"slices"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

const (
	keyCtrlC    = "ctrl+c"
	keyEnter    = "enter"
	keyEsc      = "esc"
	pendingCell = "…"
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
		m.pane.GotoTop()

		return m, nil

	case actionProgressMsg:
		return m.handleActionProgress()

	case spinner.TickMsg:
		if !m.spinnerActive() {
			return m, nil
		}

		var cmd tea.Cmd

		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case tea.KeyMsg:
		if msg.String() == keyCtrlC {
			return m, tea.Quit
		}

		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) spinnerActive() bool {
	return m.loading || m.loadingMore || (m.screen == screenResults && m.results == nil)
}

const (
	previewHeightMargin = 6
	minListHeight       = 5
	previewListFraction = 4 // preview open: list gets at most 1/4 of the available height
)

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.table.SetWidth(m.width)
	m.pane.Width = m.width
	m.pane.Height = max(m.height-paneChrome, 1)
	m.table.SetColumns(columnsForWidth(m.width))
	m = m.syncTableHeight()

	return m
}

// syncTableHeight sizes the PR table for the current window and preview
// state: full height with no preview open, otherwise at least minListHeight
// lines and at most a quarter of the available height, leaving the rest for
// the preview panel below it.
func (m Model) syncTableHeight() Model {
	available := max(m.height-previewHeightMargin, 1)
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
	if msg.id != m.searchID {
		return m
	}

	switch {
	case msg.light:
		return m.handleLightDone(msg)
	case msg.more:
		return m.handleMoreDone(msg)
	}

	m.loading = false
	m.err = msg.err

	if msg.err == nil {
		m.prs = msg.prs
		m.selected = survivingSelection(m.selected, m.prs)
		m = m.storePaging(msg)
		m = m.refreshRows()
	}

	return m
}

// handleLightDone merges the first-pass rows into the list; it never
// replaces detailed rows, and its errors are left for the full search to report.
func (m Model) handleLightDone(msg searchDoneMsg) Model {
	if msg.err != nil {
		return m
	}

	m.total = msg.total
	m.prs = mergePRs(m.prs, msg.prs)

	return m.refreshRows()
}

func (m Model) handleMoreDone(msg searchDoneMsg) Model {
	m.loadingMore = false

	if msg.err != nil {
		m.moreErr = msg.err

		return m
	}

	m.moreErr = nil
	m.prs = mergePRs(m.prs, msg.prs)
	m = m.storePaging(msg)

	return m.refreshRows()
}

// storePaging records where the next page starts and caches everything
// loaded so far for the current query.
func (m Model) storePaging(msg searchDoneMsg) Model {
	m.total, m.endCursor, m.hasMore = msg.total, msg.cursor, msg.hasNext
	m.cache[msg.query] = results{
		prs:     m.prs,
		total:   m.total,
		cursor:  m.endCursor,
		hasMore: m.hasMore,
	}

	return m
}

// mergePRs adds incoming to prs by identity: new PRs are appended, known ones
// updated in place, except that a light row never replaces a detailed one.
// prs is not modified; it may be shared with the cache.
func mergePRs(prs, incoming []github.PR) []github.PR {
	merged := slices.Clone(prs)
	index := make(map[prKey]int, len(merged))

	for pos, existing := range merged {
		index[keyOf(existing)] = pos
	}

	for _, fresh := range incoming {
		pos, known := index[keyOf(fresh)]

		switch {
		case !known:
			index[keyOf(fresh)] = len(merged)
			merged = append(merged, fresh)
		case fresh.Detailed || !merged[pos].Detailed:
			merged[pos] = fresh
		}
	}

	return merged
}

// maybeLoadMore fetches the next page once the cursor is within
// loadMoreMargin rows of the last loaded one. A failed page is retried by the
// next cursor move.
func (m Model) maybeLoadMore() (Model, tea.Cmd) {
	if !m.hasMore || m.loading || m.loadingMore || m.table.Cursor() < len(m.prs)-loadMoreMargin {
		return m, nil
	}

	m.loadingMore = true
	m.moreErr = nil

	return m, m.moreCmds()
}

func survivingSelection(selected map[prKey]bool, prs []github.PR) map[prKey]bool {
	kept := map[prKey]bool{}

	for _, pr := range prs {
		if selected[keyOf(pr)] {
			kept[keyOf(pr)] = true
		}
	}

	return kept
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
	if m.results == nil {
		return m, nil // still running; only ctrl+c (handled in Update) leaves
	}

	if msg.String() == keyEnter || msg.String() == keyEsc {
		m.screen = screenList
		m = m.withResults(results{})
		m.selected = map[prKey]bool{}
		delete(m.cache, m.query)
		m = m.refreshRows()

		return m.reload()
	}

	return m.scrollPane(msg, m.resultsBody()), nil
}

// reload starts a fresh search for the current query, cancelling any still
// running. Rows already on screen stay until the new result replaces them.
func (m Model) reload() (Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.searchID++
	m.loading = true
	m.loadingMore = false
	m.moreErr = nil
	m.err = nil

	return m, m.searchCmds(ctx)
}

func (m Model) withResults(res results) Model {
	m.prs, m.total, m.endCursor, m.hasMore = res.prs, res.total, res.cursor, res.hasMore

	return m
}

// runQuery makes query current, selecting the tab it matches (if any),
// records it in the history and reloads the list.
func (m Model) runQuery(query string) (Model, tea.Cmd) {
	m.query = query
	m.tab = tabFor(query)
	_ = m.history.Add(query) // best effort: history must never block a search

	m = m.withResults(m.cache[query])
	m.selected = map[prKey]bool{}
	m = m.refreshRows()

	return m.reload()
}

func (m Model) handleListKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "q":
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
	m, more := m.maybeLoadMore()

	return m, tea.Batch(cmd, more)
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
	m.filterInput.CursorEnd()
	m.filterInput.Focus()
	m.filterHistory = m.history.Load()
	m.filterPos = len(m.filterHistory)

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

	return m.enterConfirm(), nil
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
	case "up":
		return m.recallHistory(-1), nil
	case "down":
		return m.recallHistory(1), nil
	}

	var cmd tea.Cmd

	m.filterInput, cmd = m.filterInput.Update(msg)

	return m, cmd
}

// recallHistory moves through the past queries by step (-1 older, +1 newer)
// and puts the result in the filter bar; past the newest it restores the
// text that was being typed.
func (m Model) recallHistory(step int) Model {
	next := m.filterPos + step
	if next < 0 || next > len(m.filterHistory) {
		return m
	}

	if m.filterPos == len(m.filterHistory) {
		m.filterDraft = m.filterInput.Value()
	}

	m.filterPos = next

	value := m.filterDraft
	if next < len(m.filterHistory) {
		value = m.filterHistory[next]
	}

	m.filterInput.SetValue(value)
	m.filterInput.CursorEnd()

	return m
}

func (m Model) handleActionInputKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		input := m.actionInput.Value()
		m.actionInput.Blur()
		m.action = actionsForKey(m.client, m.actionKey, input)

		return m.enterConfirm(), nil
	case keyEsc:
		m.actionInput.Blur()
		m.screen = screenList

		return m, nil
	}

	var cmd tea.Cmd

	m.actionInput, cmd = m.actionInput.Update(msg)

	return m, cmd
}

func (m Model) enterConfirm() Model {
	m.confirm = m.selectedPRs()
	m.screen = screenConfirm
	m.pane.GotoTop()

	return m
}

// scrollPane scrolls the confirm/results list for keys that aren't the
// screen's own: j/k, arrows, page keys via the viewport, plus g/G for top and bottom.
func (m Model) scrollPane(msg tea.KeyMsg, body string) Model {
	m.pane.SetContent(body)

	switch msg.String() {
	case "g", "home":
		m.pane.GotoTop()
	case "G", "end":
		m.pane.GotoBottom()
	default:
		m.pane, _ = m.pane.Update(msg)
	}

	return m
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch key := msg.String(); {
	case key == "y" || (key == keyEnter && !m.action.destructive):
		m.screen = screenResults
		m.results = nil
		m.actionDone = new(atomic.Int32)
		m.actionTotal = len(m.confirm)

		return m, tea.Batch(m.runAction(), pollActionProgress(), m.spinner.Tick)
	case key == "n" || key == keyEsc:
		m.screen = screenList

		return m, nil
	}

	return m.scrollPane(msg, m.confirmBody()), nil
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

// rowsFor builds the table rows; for PRs that aren't detailed yet, the checks
// and merge cells are placeholders because the light search doesn't fetch them.
func rowsFor(prs []github.PR, selected map[prKey]bool) []table.Row {
	rows := make([]table.Row, len(prs))
	for idx, entry := range prs {
		mark := " "
		if selected[keyOf(entry)] {
			mark = "x"
		}

		checks, merge := pendingCell, pendingCell
		if entry.Detailed {
			checks, merge = checksSummary(entry), mergeLabel(entry.MergeState)
		}

		rows[idx] = table.Row{
			mark,
			entry.Repo,
			prNumber(entry.Number),
			entry.Title,
			checks,
			merge,
			entry.Author,
		}
	}

	return rows
}

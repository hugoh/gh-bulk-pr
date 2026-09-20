package ui

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
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
	case modelMsg:
		return msg.applyTo(m), nil
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

	case tea.MouseWheelMsg:
		return m.handleMouse(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// modelMsg is a message that only updates the model, with no command to run.
type modelMsg interface {
	applyTo(m Model) Model
}

func (msg historyLoadedMsg) applyTo(m Model) Model { return m.handleHistoryLoaded(msg) }

func (msg openFailedMsg) applyTo(m Model) Model {
	m.notice = "couldn't open PR: " + msg.err.Error()

	return m
}

// handleHistoryLoaded gives the open filter bar its past queries; a message
// that arrives after the bar was closed has nothing to do.
func (m Model) handleHistoryLoaded(msg historyLoadedMsg) Model {
	if m.screen == screenFilter {
		m.filterHistory = msg.entries
		m.filterPos = len(msg.entries)
	}

	return m
}

// wheelStep is how many rows one wheel notch moves the list cursor.
const wheelStep = 3

// wheelDelta is the cursor movement for a wheel button: rows down, negative
// for up, 0 for any other button.
func wheelDelta(button tea.MouseButton) int {
	if button == tea.MouseWheelUp {
		return -wheelStep
	}

	if button == tea.MouseWheelDown {
		return wheelStep
	}

	return 0
}

// handleMouse scrolls with the wheel (only when started with --mouse): the
// list cursor, or the confirm/results pane. Everything else is ignored.
func (m Model) handleMouse(msg tea.MouseWheelMsg) (Model, tea.Cmd) {
	if m.screen == screenConfirm {
		return m.scrollPane(msg, m.confirmBody()), nil
	}

	if m.screen == screenResults && m.results != nil {
		return m.scrollPane(msg, m.resultsBody()), nil
	}

	if m.screen != screenList {
		return m, nil
	}

	delta := wheelDelta(msg.Button)
	if delta == 0 {
		return m, nil
	}

	if delta < 0 {
		m.table.MoveUp(-delta)
	} else {
		m.table.MoveDown(delta)
	}

	return m.maybeLoadMore()
}

func (m Model) spinnerActive() bool {
	return m.loading || m.loadingMore || (m.screen == screenResults && m.results == nil)
}

const (
	// promptInputMargin leaves room around a prompt's input: its label (up to
	// "Label for 1000 PR(s): "), the padding and the cursor.
	promptInputMargin = 28
	minInputWidth     = 10

	previewHeightMargin = 6
	minListHeight       = 5
	previewListFraction = 4 // preview open: list gets at most 1/4 of the available height
)

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.table.SetWidth(m.width)
	m.pane.SetWidth(m.width)
	m.filterInput.SetWidth(max(m.width-promptInputMargin, minInputWidth))
	m.actionInput.SetWidth(max(m.width-promptInputMargin, minInputWidth))
	m.pane.SetHeight(max(m.height-paneChrome, 1))
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
	pending := m.loading
	if msg.more {
		pending = m.loadingMore
	}

	// Once the full result for this page has landed the light one has nothing
	// to add, and any row it would append that the full page lacks (a PR
	// updated in between) would stay a placeholder.
	if msg.err != nil || !pending {
		return m
	}

	m.total = msg.total
	m.prs = mergePRs(m.prs, msg.prs)

	return m.refreshRows()
}

func (m Model) handleMoreDone(msg searchDoneMsg) Model {
	m.loadingMore = false

	if m.cancelMore != nil {
		m.cancelMore()
		m.cancelMore = nil
	}

	// Rows still lacking details belong to this page's light result. Whatever
	// the full result didn't confirm (or all of them, if it failed) goes; a
	// retry fetches the page again.
	if msg.err == nil {
		m.moreErr = nil
		m.prs = mergePRs(m.prs, msg.prs)
		m = m.storePaging(msg)
	} else {
		m.moreErr = msg.err
	}

	m.prs = slices.DeleteFunc(m.prs, func(pr github.PR) bool { return !pr.Detailed })
	m.selected = survivingSelection(m.selected, m.prs)

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

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelMore = cancel
	m.loadingMore = true
	m.moreErr = nil

	return m, m.moreCmds(ctx)
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

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if msg.String() == keyCtrlC {
		return m, tea.Quit
	}

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
	case screenHelp:
		m.screen = screenList

		return m, nil
	}

	return m, nil
}

func (m Model) handleResultsKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
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

	if m.cancelMore != nil {
		m.cancelMore()
		m.cancelMore = nil
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

	m = m.withResults(m.cache[query])
	m.selected = map[prKey]bool{}
	m = m.refreshRows()
	m, search := m.reload()

	return m, tea.Batch(m.recordQuery(query), search)
}

func (m Model) handleListKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	keys := m.keys
	armed, openArmed := m.quitArmed, m.openArmed
	m.quitArmed, m.openArmed, m.notice = false, false, ""

	switch {
	case key.Matches(msg, keys.Quit):
		return m.requestQuit(armed)
	case key.Matches(msg, keys.Filter):
		return m.startFilter()
	case key.Matches(msg, keys.Help):
		m.screen = screenHelp

		return m, nil
	case key.Matches(msg, keys.Clear):
		return m.clearSelectionOrPreview()
	case key.Matches(msg, keys.Preview):
		m.previewOpen = !m.previewOpen
		m = m.syncTableHeight()

		return m, nil
	case key.Matches(msg, keys.Select):
		return m.toggleFocusedSelection()
	case key.Matches(msg, keys.SelectAll):
		return m.selectAll()
	case key.Matches(msg, keys.Refresh):
		return m.reload()
	case key.Matches(msg, keys.Checks, keys.Open, keys.OpenAll, keys.Tab, keys.Label, keys.Close, keys.Merge):
		return m.handleCommandKey(msg, openArmed)
	}

	var cmd tea.Cmd

	m.table, cmd = m.table.Update(msg)
	m, more := m.maybeLoadMore()

	return m, tea.Batch(cmd, more)
}

// requestQuit quits at once, unless PRs are selected: then the first q asks
// for a second (any other key cancels), so a stray q doesn't lose the selection.
func (m Model) requestQuit(armed bool) (Model, tea.Cmd) {
	if len(m.selected) > 0 && !armed {
		m.quitArmed = true

		return m, nil
	}

	return m, tea.Quit
}

const (
	// openAllConfirmAbove is the most PRs O opens without asking first.
	openAllConfirmAbove = 5
	// openAllMax is the most it will open at all: a browser tab each is hard to undo.
	openAllMax = 20
)

// openSelected opens every selected PR that has a URL in the browser. Up to
// openAllConfirmAbove go straight away, more need a second O (armed), and
// more than openAllMax are refused.
func (m Model) openSelected(armed bool) (Model, tea.Cmd) {
	var urls []string

	for _, pr := range m.selectedPRs() {
		if pr.URL != "" {
			urls = append(urls, pr.URL)
		}
	}

	switch {
	case len(urls) == 0:
		return m, nil
	case len(urls) > openAllMax:
		m.notice = fmt.Sprintf("%d selected: O can open at most %d at once", len(urls), openAllMax)

		return m, nil
	case len(urls) > openAllConfirmAbove && !armed:
		m.openArmed = true

		return m, nil
	}

	return m, m.openAll(urls)
}

func (m Model) handleCommandKey(msg tea.KeyPressMsg, openArmed bool) (Model, tea.Cmd) {
	pressed := msg.String()

	switch {
	case key.Matches(msg, m.keys.Checks):
		return m, m.launchEnhance()
	case key.Matches(msg, m.keys.Open):
		return m, m.openFocused()
	case key.Matches(msg, m.keys.OpenAll):
		return m.openSelected(openArmed)
	case key.Matches(msg, m.keys.Tab):
		return m.runQuery(tabs()[int(pressed[0]-'1')].query)
	default:
		return m.startAction(pressed)
	}
}

func (m Model) startFilter() (Model, tea.Cmd) {
	m.screen = screenFilter
	m.filterInput.SetValue(m.query)
	m.filterInput.CursorEnd()
	m.filterInput.Focus()
	m.filterHistory = nil
	m.filterPos = 0

	return m, m.loadHistory()
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
		if id := keyOf(pr); m.selected[id] {
			delete(m.selected, id)
		} else {
			m.selected[id] = true
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

func (m Model) startAction(actionKey string) (Model, tea.Cmd) {
	if len(m.selected) == 0 {
		return m, nil
	}

	if needsInput(actionKey) {
		m.actionKey = actionKey
		m.screen = screenActionInput
		m.actionInput.SetValue("")
		m.actionInput.Focus()

		return m, nil
	}

	m.action = actionsForKey(m.client, actionKey, "")

	return m.enterConfirm(), nil
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
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

func (m Model) handleActionInputKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
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
func (m Model) scrollPane(msg tea.Msg, body string) Model {
	m.pane.SetContent(body)

	if pressed, isKey := msg.(tea.KeyPressMsg); isKey {
		switch pressed.String() {
		case "g", "home":
			m.pane.GotoTop()

			return m
		case "G", "end":
			m.pane.GotoBottom()

			return m
		}
	}

	m.pane, _ = m.pane.Update(msg)

	return m
}

func (m Model) handleConfirmKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch pressed := msg.String(); {
	case pressed == "y" || (pressed == keyEnter && !m.action.destructive):
		m.screen = screenResults
		m.results = nil
		m.actionDone = new(atomic.Int32)
		m.actionTotal = len(m.confirm)

		return m, tea.Batch(m.runAction(), pollActionProgress(), m.spinner.Tick)
	case pressed == "n" || pressed == keyEsc:
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

package ui

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

const (
	// detailBatchSize is how many PRs one details request asks for. A request
	// costs one API point whatever its size, but takes about 0.2s per PR, so
	// small batches put the first rows on screen sooner.
	detailBatchSize = 10
	// detailConcurrency is how many details requests run at once.
	detailConcurrency = 4
	// detailLookahead is how many screens below the cursor are fetched ahead.
	detailLookahead = 2
)

type fetchState int

const (
	fetchNone fetchState = iota
	fetchInflight
	fetchDone
	fetchFailed
)

// detailsDoneMsg carries one details batch. searchID says which search it was
// asked for: a reload abandons them.
type detailsDoneMsg struct {
	searchID int
	ids      []string
	details  []github.Detail
	err      error
}

func (m Model) detailsCmd(ids []string) tea.Cmd {
	client, ctx, searchID := m.client, m.detailCtx, m.searchID

	return func() tea.Msg {
		details, err := client.Details(ctx, ids)

		return detailsDoneMsg{searchID: searchID, ids: ids, details: details, err: err}
	}
}

// wantedDetails lists the loaded PRs whose details are still to be fetched,
// nearest the cursor first. It is empty until one lies within a screen of the
// cursor, so that scrolling asks for a batch of rows at a time instead of one
// request per keypress.
func (m Model) wantedDetails() []string {
	cursor, screen := m.table.Cursor(), max(m.table.Height(), 1)
	first, end := max(cursor-screen, 0), min(cursor+detailLookahead*screen+1, len(m.prs))

	var rows []int

	near := false

	for row := first; row < end; row++ {
		if id := m.prs[row].ID; id != "" && m.fetched[id] == fetchNone {
			rows = append(rows, row)
			near = near || abs(row-cursor) <= screen
		}
	}

	if !near {
		return nil
	}

	slices.SortStableFunc(
		rows,
		func(a, b int) int { return cmp.Compare(abs(a-cursor), abs(b-cursor)) },
	)

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = m.prs[row].ID
	}

	return ids
}

func abs(n int) int { return max(n, -n) }

// fetchDetails starts details requests for the wanted rows, up to
// detailConcurrency at a time.
func (m Model) fetchDetails() (Model, tea.Cmd) {
	slots := detailConcurrency - m.fetching
	if slots <= 0 {
		return m, nil
	}

	var cmds []tea.Cmd

	for batch := range slices.Chunk(m.wantedDetails(), detailBatchSize) {
		if len(cmds) == slots {
			break
		}

		for _, id := range batch {
			m.fetched[id] = fetchInflight
		}

		m.fetching++
		cmds = append(cmds, m.detailsCmd(batch))
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleDetailsDone(msg detailsDoneMsg) (Model, tea.Cmd) {
	if msg.searchID != m.searchID {
		return m, nil
	}

	m.fetching--

	if msg.err != nil {
		for _, id := range msg.ids {
			m.fetched[id] = fetchFailed
		}

		m.detailErr = msg.err

		return m, nil
	}

	m.detailErr = nil

	for _, id := range msg.ids {
		m.fetched[id] = fetchDone
	}

	for _, detail := range msg.details {
		m.details[detail.ID] = detail
	}

	m = m.withStoredDetails().refreshRows()

	return m.fetchDetails()
}

// withStoredDetails fills in every listed PR whose details are known, however
// old, so a refresh doesn't blank the Checks and Merge columns while it loads.
// m.prs may be shared with the cache, so it is copied, not edited.
func (m Model) withStoredDetails() Model {
	prs := slices.Clone(m.prs)

	for i, pr := range prs {
		if detail, ok := m.details[pr.ID]; ok {
			prs[i] = pr.WithDetail(detail)
		}
	}

	m.prs = prs

	return m
}

// followCursor is what moving the cursor sets off: the next page when it is
// near the end, and the details of the rows it approaches. Rows whose details
// failed to load are tried again.
func (m Model) followCursor() (Model, tea.Cmd) {
	for id, state := range m.fetched {
		if state == fetchFailed {
			delete(m.fetched, id)
		}
	}

	m, more := m.maybeLoadMore()
	m, details := m.fetchDetails()

	return m, tea.Batch(more, details)
}

// appendNew adds the PRs of incoming that prs doesn't list yet: a PR updated
// while the pages were being fetched moves up the results, so it can turn up
// again on a later page. prs is not modified; it may be shared with the cache.
func appendNew(prs, incoming []github.PR) []github.PR {
	known := make(map[prKey]bool, len(prs)+len(incoming))
	for _, pr := range prs {
		known[keyOf(pr)] = true
	}

	merged := slices.Clone(prs)

	for _, pr := range incoming {
		if !known[keyOf(pr)] {
			known[keyOf(pr)] = true
			merged = append(merged, pr)
		}
	}

	return merged
}

package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

// searchDoneMsg carries one page of search results. id and query say which
// search it answers, and more marks a page after the first.
type searchDoneMsg struct {
	github.Page

	id    int
	query string
	more  bool
	err   error
}

// searchCmds fetches the first page of results. Its rows arrive without
// details, which fetchDetails asks for as they land. It stops with ctx.
func (m Model) searchCmds(ctx context.Context) tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runSearch(ctx, "", false))
}

// moreCmds fetches the page after m.endCursor. It stops with ctx.
func (m Model) moreCmds(ctx context.Context) tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runSearch(ctx, m.endCursor, true))
}

func (m Model) runSearch(ctx context.Context, after string, more bool) tea.Cmd {
	client, query, searchID := m.client, m.query, m.searchID

	return func() tea.Msg {
		page, err := client.SearchPage(ctx, query, after)

		return searchDoneMsg{Page: page, id: searchID, query: query, more: more, err: err}
	}
}

func (m Model) handleSearchDone(msg searchDoneMsg) Model {
	if msg.id != m.searchID {
		return m
	}

	if msg.more {
		return m.handleMoreDone(msg)
	}

	m.loading = false
	m.err = msg.err

	if msg.err == nil {
		m.prs, m.hidden = msg.PRs, 0
		m = m.withStoredDetails()
		m.selected = survivingSelection(m.selected, m.prs)
		m = m.storePaging(msg)
		m = m.refreshRows()
		m.lastUpdated = time.Now()
	}

	return m
}

func (m Model) handleMoreDone(msg searchDoneMsg) Model {
	m.loadingMore = false

	if m.cancelMore != nil {
		m.cancelMore()
		m.cancelMore = nil
	}

	if msg.err != nil {
		m.moreErr = msg.err

		return m
	}

	m.moreErr = nil
	m.prs = appendNew(m.prs, msg.PRs)
	m = m.withStoredDetails()
	m = m.storePaging(msg)
	m.lastUpdated = time.Now()

	return m.refreshRows()
}

// storePaging records where the next page starts and caches everything
// loaded so far for the current query.
func (m Model) storePaging(msg searchDoneMsg) Model {
	m.total, m.endCursor, m.hasMore = max(msg.Total-m.hidden, 0), msg.EndCursor, msg.HasNext
	m.cache[msg.query] = github.Page{
		PRs:       m.prs,
		Total:     m.total,
		EndCursor: m.endCursor,
		HasNext:   m.hasMore,
	}

	return m
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
	m.detailErr = nil

	for id, state := range m.fetched {
		if state != fetchInflight {
			delete(m.fetched, id)
		}
	}

	m.searchID++
	m.loading = true
	m.loadingMore = false
	m.moreErr = nil
	m.err = nil

	return m, m.searchCmds(ctx)
}

// refreshAll reloads the whole list and, without waiting for it, re-fetches
// the details of the focused PR unless a request for them is in flight.
func (m Model) refreshAll() (Model, tea.Cmd) {
	m, search := m.reload()

	pr, ok := m.focusedPR()
	if !ok || pr.ID == "" || m.fetched[pr.ID] == fetchInflight {
		return m, search
	}

	m, focused := m.fetchIDs([]string{pr.ID})

	return m, tea.Batch(search, focused)
}

func (m Model) withResults(page github.Page) Model {
	m.prs, m.total, m.endCursor, m.hasMore = page.PRs, page.Total, page.EndCursor, page.HasNext

	return m.withStoredDetails()
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

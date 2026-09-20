package ui

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func asModel(t *testing.T, model tea.Model) Model {
	t.Helper()

	m, ok := model.(Model)
	require.True(t, ok)

	return m
}

func idOf(number int) string { return fmt.Sprintf("PR_%d", number) }

// detailModel is 50 rows as the search leaves them, before any details.
func detailModel(t *testing.T) Model {
	t.Helper()

	return detailModelSized(t, 30)
}

func detailModelSized(t *testing.T, height int) Model {
	t.Helper()

	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: height})

	return m.handleSearchDone(searchDoneMsg{
		PRs:   manyPRs(1, 50, false),
		query: "q",
	})
}

func countState(m Model, state fetchState) int {
	n := 0

	for _, got := range m.fetched {
		if got == state {
			n++
		}
	}

	return n
}

func detailFor(number int) github.Detail {
	return github.Detail{
		ID:         idOf(number),
		MergeState: github.MergeClean,
		Checks:     github.ChecksPass,
		Body:       "body",
	}
}

func TestWantedDetails_NearestTheCursorFirst(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m.table.SetCursor(20)

	wanted := m.wantedDetails()

	require.NotEmpty(t, wanted)
	assert.Equal(t, []string{idOf(21), idOf(20), idOf(22)}, wanted[:3])
}

func TestWantedDetails_OnlyFromTheWindowAroundTheCursor(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	span := m.table.Height()

	assert.Len(
		t,
		m.wantedDetails(),
		min(50, detailLookahead*span+1),
		"cursor 0: rows 1.. up to the lookahead",
	)
	assert.NotContains(t, m.wantedDetails(), idOf(50))
}

func TestWantedDetails_SkipsRowsWithoutAnID(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m.prs[0].ID = ""

	assert.NotContains(t, m.wantedDetails(), "")
}

func TestWantedDetails_WaitsUntilARowNearTheCursorIsMissing(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	span := m.table.Height()

	for i := 0; i <= span; i++ {
		m.fetched[idOf(i+1)] = fetchDone
	}

	assert.Empty(
		t,
		m.wantedDetails(),
		"everything within a screen of the cursor is loaded or on its way",
	)

	m.table.SetCursor(2)
	assert.NotEmpty(t, m.wantedDetails(), "scrolling brings an unloaded row within a screen")
}

func TestFetchDetails_BoundedBatchesNearestFirst(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, cmd := m.fetchDetails()

	require.NotNil(t, cmd)
	assert.Equal(t, detailConcurrency, m.fetching)
	assert.Equal(t, detailConcurrency*detailBatchSize, countState(m, fetchInflight))

	for number := 1; number <= detailConcurrency*detailBatchSize; number++ {
		assert.Equal(t, fetchInflight, m.fetched[idOf(number)], "row %d", number)
	}

	assert.Equal(t, fetchNone, m.fetched[idOf(detailConcurrency*detailBatchSize+1)])

	_, again := m.fetchDetails()
	assert.Nil(t, again, "every slot is busy")
}

func TestHandleDetailsDone_FillsRowsAndKeepsGoing(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.fetchDetails()

	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID,
		ids:      []string{idOf(1), idOf(2)},
		details:  []github.Detail{detailFor(1)},
	})

	assert.True(t, m.prs[0].Detailed)
	assert.Equal(t, github.MergeClean, m.prs[0].MergeState)
	assert.Equal(t, "clean", m.table.Rows()[0][5], "the table shows it without a reload")
	assert.False(t, m.prs[1].Detailed, "an ID GitHub no longer returns stays a placeholder")
	assert.Equal(t, fetchDone, m.fetched[idOf(2)], "and is not asked for again")
	assert.Equal(t, detailConcurrency-1, m.fetching)
}

func TestHandleDetailsDone_FreedSlotPicksUpTheNextRows(t *testing.T) {
	t.Parallel()

	m := detailModelSized(t, 90)
	require.Greater(
		t,
		m.table.Height(),
		detailConcurrency*detailBatchSize,
		"more rows are near than one round covers",
	)

	m, _ = m.fetchDetails()
	require.Equal(t, fetchNone, m.fetched[idOf(detailConcurrency*detailBatchSize+1)])

	m, cmd := m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID,
		ids:      []string{idOf(1)},
		details:  []github.Detail{detailFor(1), detailFor(2)},
	})

	assert.NotNil(t, cmd)
	assert.Equal(t, detailConcurrency, m.fetching)
	assert.Equal(t, fetchInflight, m.fetched[idOf(detailConcurrency*detailBatchSize+1)])
}

func TestHandleDetailsDone_FailureLeavesRowsRetryable(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.fetchDetails()

	failed := []string{idOf(1), idOf(2)}
	m, cmd := m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID,
		ids:      failed,
		err:      errors.New("502"),
	})

	assert.Nil(t, cmd, "a failure does not retry by itself")
	require.Error(t, m.detailErr)
	assert.False(t, m.prs[0].Detailed)
	assert.Equal(t, fetchFailed, m.fetched[idOf(1)])
	assert.Equal(t, detailConcurrency-1, m.fetching)
	assert.NotContains(t, m.wantedDetails(), idOf(1), "not until the cursor moves")

	m, cmd = m.followCursor()
	assert.NotNil(t, cmd)
	assert.Equal(t, fetchInflight, m.fetched[idOf(1)], "moving retries the failed rows")
}

func TestHandleDetailsDone_IgnoresAnAbandonedSearch(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.fetchDetails()
	stale := m.searchID

	m, _ = m.reload()
	m, cmd := m.handleDetailsDone(detailsDoneMsg{
		searchID: stale,
		ids:      []string{idOf(1)},
		details:  []github.Detail{detailFor(1)},
	})

	assert.Nil(t, cmd)
	assert.False(t, m.prs[0].Detailed)
	assert.Zero(t, m.fetching, "the reload already forgot those requests")
}

func TestReload_KeepsKnownDetailsButFetchesThemAgain(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.fetchDetails()
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID,
		ids:      []string{idOf(1)},
		details:  []github.Detail{detailFor(1)},
	})

	m, _ = m.reload()
	assert.Zero(t, countState(m, fetchDone)+countState(m, fetchInflight), "a refresh starts over")

	m = m.handleSearchDone(searchDoneMsg{
		PRs: manyPRs(1, 50, false),
		id:  m.searchID, query: "q",
	})

	assert.True(t, m.prs[0].Detailed, "the last known state shows while the new one loads")
	assert.Equal(t, "clean", m.table.Rows()[0][5])
	assert.Contains(t, m.wantedDetails(), idOf(1))
}

func TestUpdate_SearchDoneStartsFetchingDetails(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	updated, cmd := m.Update(searchDoneMsg{PRs: manyPRs(1, 50, false)})

	require.NotNil(t, cmd)
	assert.Equal(t, detailConcurrency, asModel(t, updated).fetching)
}

func TestUpdate_RoutesDetailsDone(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.fetchDetails()

	updated, _ := m.Update(detailsDoneMsg{
		searchID: m.searchID,
		ids:      []string{idOf(1)},
		details:  []github.Detail{detailFor(1)},
	})

	assert.True(t, asModel(t, updated).prs[0].Detailed)
}

func TestMovingTheCursorFetchesDetails(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, cmd := m.handleListKeyByString("j")

	assert.NotNil(t, cmd)
	assert.Positive(t, m.fetching)
}

func TestDetailFailureIsShown(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m.detailErr = errors.New("502 Bad Gateway")

	assert.Contains(t, m.statusLine(), "502 Bad Gateway")
}

func TestAppendNew(t *testing.T) {
	t.Parallel()

	have := manyPRs(1, 3, false)
	got := appendNew(have, manyPRs(3, 3, false))

	assert.Len(t, got, 5, "the page shifted by one, so PR 3 is not listed twice")
	assert.Equal(t, 5, got[4].Number)
	assert.Len(t, have, 3, "the input may be shared with the cache")
}

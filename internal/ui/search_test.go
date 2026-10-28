package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSearchDone(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		m := New(nil, "q")
		m = m.handleSearchDone(searchDoneMsg{PRs: testPRs()})

		assert.False(t, m.loading)
		require.NoError(t, m.err)
		assert.Equal(t, testPRs(), m.prs)
		assert.Len(t, m.table.Rows(), 2)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		m := New(nil, "q")
		m = m.handleSearchDone(searchDoneMsg{err: assert.AnError})

		assert.False(t, m.loading)
		require.ErrorIs(t, m.err, assert.AnError)
		assert.Empty(t, m.prs)
	})
}

func TestQueryChangesAreRecorded(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	m := loadedModel().WithHistory(history.New(path))

	record := func(query string) {
		var cmd tea.Cmd

		m, cmd = m.runQuery(query)

		batch, ok := cmd().(tea.BatchMsg)
		require.True(t, ok)
		require.NotEmpty(t, batch)

		// The history write is the first command of the batch.
		assert.Nil(t, batch[0](), "recording a query produces no message")
	}

	record(tabs()[0].query)
	record("is:open is:pr author:hugoh")

	got, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	require.NoError(t, err)
	assert.Equal(
		t,
		"is:open is:pr archived:false sort:updated-desc owner:@me\nis:open is:pr author:hugoh\n",
		string(got),
	)
}

func TestRunQuery_DoesNoFileIOItself(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	m := loadedModel().WithHistory(history.New(path))

	_, cmd := m.runQuery("is:open")

	require.NotNil(t, cmd)
	assert.NoFileExists(t, path, "the write happens when the command runs, not inside Update")
}

func TestRunQuery_WithoutHistory(t *testing.T) {
	t.Parallel()

	_, cmd := loadedModel().runQuery("is:open")

	assert.NotNil(t, cmd, "the search still runs")
}

func TestSearchIgnoresStaleResults(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = reloaded(t, m)
	firstID := m.searchID
	m, _ = reloaded(t, m)
	require.Greater(t, m.searchID, firstID)

	m = m.handleSearchDone(
		searchDoneMsg{PRs: []github.PR{{Number: 99, Repo: testRepoA}}, id: firstID},
	)

	assert.True(t, m.loading, "stale result must not end the loading state")
	assert.Equal(t, testPRs(), m.prs, "stale result must not replace the list")
}

func TestReloadCancelsPreviousSearch(t *testing.T) {
	t.Parallel()

	canceled := false
	m := loadedModel()
	m.cancel = func() { canceled = true }

	_, _ = reloaded(t, m)

	assert.True(t, canceled)
}

func TestRefreshKeepsSelectionOfSurvivingPRs(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true
	m.selected[keyOf(m.prs[1])] = true

	m = m.handleSearchDone(searchDoneMsg{PRs: testPRs()[:1]})

	assert.Equal(t, map[prKey]bool{keyOf(testPRs()[0]): true}, m.selected)
}

func TestReloadKeepsRows(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = reloaded(t, m)

	assert.Equal(t, testPRs(), m.prs, "refresh keeps showing the current rows")
}

func TestFullPageStoresPaging(t *testing.T) {
	t.Parallel()

	m := pagedModel()

	assert.Equal(t, 312, m.total)
	assert.Equal(t, "c1", m.endCursor)
	assert.True(t, m.hasMore)
	assert.Equal(
		t,
		github.Page{PRs: m.prs, Total: 312, EndCursor: "c1", HasNext: true},
		m.cache["q"],
	)
}

func TestLoadMore_DedupesShiftedResults(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{PRs: manyPRs(50, 50, true), more: true})

	assert.Len(t, m.prs, 99, "PR 50 was already on the first page")
}

func TestLoadMore_ErrorKeepsRows(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{more: true, err: assert.AnError})

	require.NoError(t, m.err, "the list itself is still fine")
	require.ErrorIs(t, m.moreErr, assert.AnError)
	assert.False(t, m.loadingMore)
	assert.Len(t, m.prs, 50)
	assert.Contains(t, m.View().Content, "load more failed")
}

func TestLoadMore_StaleResultDroppedAfterReload(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true
	staleID := m.searchID

	m, _ = reloaded(t, m)
	assert.False(t, m.loadingMore, "a new search abandons the in-flight page")

	m = m.handleSearchDone(searchDoneMsg{PRs: manyPRs(51, 50, true), id: staleID, more: true})
	assert.Len(t, m.prs, 50)
}

func TestMaybeLoadMore(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate func(*Model)
		cursor int
		want   bool
	}{
		"near the bottom":     {cursor: 50 - loadMoreMargin, want: true},
		"far from the bottom": {cursor: 0, want: false},
		"nothing more to load": {
			cursor: 49,
			mutate: func(m *Model) { m.hasMore = false },
			want:   false,
		},
		"already loading more": {
			cursor: 49,
			mutate: func(m *Model) { m.loadingMore = true },
			want:   false,
		},
		"first search running": {
			cursor: 49,
			mutate: func(m *Model) { m.loading = true },
			want:   false,
		},
		"retries after an error": {
			cursor: 49,
			mutate: func(m *Model) { m.moreErr = assert.AnError },
			want:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := pagedModel()
			m.table.SetCursor(tt.cursor)

			if tt.mutate != nil {
				tt.mutate(&m)
			}

			got, cmd := m.maybeLoadMore()

			assert.Equal(t, tt.want, cmd != nil)

			if tt.want {
				assert.True(t, got.loadingMore)
				assert.NoError(t, got.moreErr, "a retry clears the old error")
			}
		})
	}
}

func TestReloadCancelsTheInFlightMorePage(t *testing.T) {
	t.Parallel()

	canceled := false
	m := pagedModel()
	m.cancelMore = func() { canceled = true }

	m, _ = reloaded(t, m)

	assert.True(t, canceled, "a reload abandons the page being fetched")
	assert.Nil(t, m.cancelMore)
}

func TestLoadingMoreKeepsACancelFunc(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.table.SetCursor(49)

	m, cmd := m.maybeLoadMore()

	require.NotNil(t, cmd)
	assert.NotNil(t, m.cancelMore)
}

func TestFinishedMorePageReleasesItsContext(t *testing.T) {
	t.Parallel()

	released := false
	m := pagedModel()
	m.loadingMore = true
	m.cancelMore = func() { released = true }

	m = m.handleSearchDone(searchDoneMsg{PRs: manyPRs(51, 1, true), query: "q", more: true})

	assert.True(t, released)
	assert.Nil(t, m.cancelMore)
}

func TestLoadMore_AppendsThePageAndCachesIt(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{
		PRs: manyPRs(51, 50, false), Total: 312, EndCursor: "c2", HasNext: true,
		query: "q",
		more:  true,
	})

	assert.Len(t, m.prs, 100)
	assert.Equal(t, "…", m.table.Rows()[50][4], "the new rows wait for their details")
	assert.False(t, m.loadingMore)
	assert.Equal(t, "c2", m.endCursor)
	assert.True(t, m.hasMore)
	assert.Len(t, m.cache["q"].PRs, 100)
	assert.Equal(t, "c2", m.cache["q"].EndCursor)
}

func TestSearchDone_ShowsRowsWithPlaceholdersUntilDetailsLand(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").handleSearchDone(searchDoneMsg{PRs: manyPRs(1, 3, false)})

	assert.False(t, m.loading)
	assert.Equal(t, "…", m.table.Rows()[0][4])
	assert.Equal(t, "…", m.table.Rows()[0][5])
}

func TestHandleSearchDone_CursorFollowsFocusedPR(t *testing.T) {
	t.Parallel()

	first := github.PR{Number: 1, Repo: testRepoA, Title: "one"}
	second := github.PR{Number: 2, Repo: testRepoB, Title: "two"}
	third := github.PR{Number: 3, Repo: testRepoB, Title: "three"}

	tests := []struct {
		name   string
		result []github.PR
		want   int
	}{
		{"row moved down", []github.PR{first, third, second}, 2},
		{"row moved up", []github.PR{second, first, third}, 0},
		{"row gone", []github.PR{first, third}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 110, Height: 30})
			m = m.handleSearchDone(searchDoneMsg{PRs: []github.PR{first, second, third}})
			m.table.SetCursor(1)

			m = m.handleSearchDone(searchDoneMsg{PRs: tt.result})

			require.Equal(t, tt.want, m.table.Cursor())
		})
	}
}

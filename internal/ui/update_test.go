package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	actionClose = "close"
	actionMerge = "merge"
	prTitleFix  = "Fix bug"
	testAuthor  = "hugoh"
	testRepoA   = "hugoh/a"
	testRepoB   = "hugoh/b"
)

func testPRs() []github.PR {
	return []github.PR{
		{
			Number:     1,
			Title:      prTitleFix,
			Repo:       testRepoA,
			Author:     testAuthor,
			MergeState: mergeBehind,
			Detailed:   true,
		},
		{Number: 2, Title: "Add feature", Repo: testRepoB, Author: testAuthor, Detailed: true},
	}
}

// loadedModel returns a Model with two PRs loaded (as handleSearchDone would
// leave it) and sized like a real terminal, so the table has rows and a
// cursor to test against.
func loadedModel() Model {
	m := New(nil, "is:open is:pr")
	m = m.handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.handleSearchDone(searchDoneMsg{prs: testPRs()})

	return m
}

// keyMsgFromString builds a tea.KeyMsg whose String() matches s, for the
// handful of key forms this app switches on ("esc", "enter", "ctrl+a",
// "ctrl+c", or a single printable rune like "x" or "/").
func keyMsgFromString(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+a":
		return tea.KeyMsg{Type: tea.KeyCtrlA}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func (m Model) handleListKeyByString(s string) (Model, tea.Cmd) {
	return m.handleListKey(keyMsgFromString(s))
}

func noopAction(context.Context, github.PR) error { return nil }

func TestHandleResize(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	m = m.handleResize(tea.WindowSizeMsg{Width: 80, Height: 24})

	assert.Equal(t, 80, m.width)
	assert.Equal(t, 24, m.height)
	assert.Equal(t, 24-previewHeightMargin-1, m.table.Height())
}

func TestSyncTableHeight(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		height      int
		previewOpen bool
		want        int
	}{
		"no preview uses full height": {
			height:      30,
			previewOpen: false,
			want:        30 - previewHeightMargin - 1,
		},
		"preview open uses a quarter": {
			height:      40,
			previewOpen: true,
			want:        (40-previewHeightMargin)/previewListFraction - 1,
		},
		"preview open clamps to the minimum": {
			height:      10,
			previewOpen: true,
			want:        minListHeight - 1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := New(nil, "q")
			m.height = tt.height
			m.previewOpen = tt.previewOpen
			m = m.syncTableHeight()

			assert.Equal(t, tt.want, m.table.Height())
		})
	}
}

func TestHandleSearchDone(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		m := New(nil, "q")
		m = m.handleSearchDone(searchDoneMsg{prs: testPRs()})

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

func TestHandleListKey_Quit(t *testing.T) {
	t.Parallel()

	m := loadedModel()

	_, cmd := m.handleListKeyByString("q")
	require.NotNil(t, cmd)

	_, ok := cmd().(tea.QuitMsg)
	assert.True(t, ok)
}

func TestCtrlCQuitsFromEveryScreen(t *testing.T) {
	t.Parallel()

	screens := map[string]screen{
		"list":         screenList,
		"filter":       screenFilter,
		"action input": screenActionInput,
		"confirm":      screenConfirm,
		"results":      screenResults,
	}

	for name, current := range screens {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.screen = current
			m.action = &pendingAction{label: actionClose, run: noopAction}
			m.results = nil // a bulk action still running

			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			require.NotNil(t, cmd)

			_, ok := cmd().(tea.QuitMsg)
			assert.True(t, ok)
		})
	}
}

func TestHandleListKey_Filter(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("/")

	assert.Equal(t, screenFilter, m.screen)
	assert.True(t, m.filterInput.Focused())
}

func TestHandleListKey_ToggleSelection(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	assert.Empty(t, m.selected)

	m, _ = m.handleListKeyByString("x")
	assert.NotEmpty(t, m.selected)
	assert.True(t, m.selected[keyOf(m.prs[0])], "toggling should select the focused (first) row")

	m, _ = m.handleListKeyByString("x")
	assert.Empty(t, m.selected, "toggling again should deselect")
}

func TestHandleListKey_SelectAll(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("ctrl+a")

	assert.Len(t, m.selectedPRs(), 2)
}

func TestHandleListKey_EscClearsSelectionThenPreview(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("x")
	require.NotEmpty(t, m.selected)

	m, _ = m.handleListKeyByString("esc")
	assert.Empty(t, m.selected, "esc clears selection first")

	m, _ = m.handleListKeyByString("p")
	require.True(t, m.previewOpen)

	m, _ = m.handleListKeyByString("esc")
	assert.False(t, m.previewOpen, "esc then closes an open preview")
}

func TestHandleListKey_TogglePreviewResizesTable(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	fullHeight := m.table.Height()

	m, _ = m.handleListKeyByString("enter")
	assert.True(t, m.previewOpen)
	assert.Less(t, m.table.Height(), fullHeight)

	m, _ = m.handleListKeyByString("enter")
	assert.False(t, m.previewOpen)
	assert.Equal(t, fullHeight, m.table.Height())
}

func TestHandleListKey_RefreshReloadsPRs(t *testing.T) {
	t.Parallel()

	m := loadedModel()

	m, cmd := m.handleListKeyByString("r")

	assert.True(t, m.loading)
	assert.Equal(t, screenList, m.screen)
	assert.NotNil(t, cmd)
}

func TestHandleListKey_StartAction_NoSelection(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, cmd := m.handleListKeyByString("c")

	assert.Equal(t, screenList, m.screen, "action keys are no-ops with nothing selected")
	assert.Nil(t, cmd)
}

func TestHandleListKey_StartAction_NeedsInput(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("x")

	m, _ = m.handleListKeyByString("l")
	assert.Equal(t, screenActionInput, m.screen)
	assert.Equal(t, "l", m.actionKey)
	assert.True(t, m.actionInput.Focused())
}

func TestHandleListKey_StartAction_NoInputNeeded(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("x")

	m, _ = m.handleListKeyByString("c")
	assert.Equal(t, screenConfirm, m.screen)
	require.Len(t, m.confirm, 1)
	assert.Equal(t, 1, m.confirm[0].Number)
}

func TestHandleFilterKey(t *testing.T) {
	t.Parallel()

	t.Run("enter runs the new query", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenFilter
		m.filterInput.SetValue("is:open author:hugoh")

		m, cmd := m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})

		assert.Equal(t, screenList, m.screen)
		assert.Equal(t, "is:open author:hugoh", m.query)
		assert.True(t, m.loading)
		require.NotNil(t, cmd)
	})

	t.Run("esc cancels without changing the query", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		original := m.query
		m.screen = screenFilter
		m.filterInput.SetValue("something else")

		m, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEsc})

		assert.Equal(t, screenList, m.screen)
		assert.Equal(t, original, m.query)
	})
}

func TestHandleActionInputKey(t *testing.T) {
	t.Parallel()

	t.Run("enter builds the action and moves to confirm", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m, _ = m.handleListKeyByString("x")
		m.screen = screenActionInput
		m.actionKey = "l"
		m.actionInput.SetValue("bug")

		m, _ = m.handleActionInputKey(tea.KeyMsg{Type: tea.KeyEnter})

		assert.Equal(t, screenConfirm, m.screen)
		require.NotNil(t, m.action)
		assert.Contains(t, m.action.label, "bug")
		require.Len(t, m.confirm, 1)
	})

	t.Run("esc cancels back to the list", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenActionInput
		m.actionKey = "l"

		m, _ = m.handleActionInputKey(tea.KeyMsg{Type: tea.KeyEsc})

		assert.Equal(t, screenList, m.screen)
	})
}

func TestHandleConfirmKey(t *testing.T) {
	t.Parallel()

	t.Run("y starts the action", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenConfirm
		m.confirm = testPRs()
		m.action = &pendingAction{label: actionClose, run: noopAction}

		m, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})

		assert.Equal(t, screenResults, m.screen)
		assert.Nil(t, m.results)
		require.NotNil(t, m.actionDone)
		assert.Equal(t, 2, m.actionTotal)
		require.NotNil(t, cmd)
	})

	t.Run("n cancels back to the list", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenConfirm

		m, _ = m.handleConfirmKey(keyMsgFromString("n"))
		assert.Equal(t, screenList, m.screen)
	})
}

func TestHandleConfirmKey_DestructiveNeedsY(t *testing.T) {
	t.Parallel()

	for _, label := range []string{actionClose, actionMerge} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.screen = screenConfirm
			m.confirm = testPRs()
			m.action = &pendingAction{label: label, run: noopAction, destructive: true}

			same, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
			assert.Equal(
				t,
				screenConfirm,
				same.screen,
				"enter must not confirm an irreversible action",
			)
			assert.Nil(t, cmd)

			started, cmd := m.handleConfirmKey(keyMsgFromString("y"))
			assert.Equal(t, screenResults, started.screen)
			assert.NotNil(t, cmd)
		})
	}
}

func TestHandleResultsKey(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenResults
	m.results = []worker.Result{}

	m, cmd := m.handleResultsKey(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, screenList, m.screen)
	assert.True(t, m.loading)
	require.NotNil(t, cmd)
}

func TestHandleResultsKey_IgnoredWhileRunning(t *testing.T) {
	t.Parallel()

	for _, keyType := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc} {
		m := loadedModel()
		m.screen = screenResults
		m.results = nil

		m, cmd := m.handleResultsKey(tea.KeyMsg{Type: keyType})

		assert.Equal(t, screenResults, m.screen, "the screen must not change mid-run")
		assert.Nil(t, cmd)
	}
}

func TestFocusedPR(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	pr, ok := m.focusedPR()
	require.True(t, ok)
	assert.Equal(t, 1, pr.Number)

	empty := New(nil, "q")
	_, ok = empty.focusedPR()
	assert.False(t, ok)
}

func TestSelectedPRs(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	assert.Empty(t, m.selectedPRs())

	m.selected[keyOf(m.prs[1])] = true
	got := m.selectedPRs()
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Number)
}

func TestRowsFor(t *testing.T) {
	t.Parallel()

	prs := testPRs()
	rows := rowsFor(prs, map[prKey]bool{keyOf(prs[1]): true})

	require.Len(t, rows, 2)
	assert.Equal(t, " ", rows[0][0])
	assert.Equal(t, "x", rows[1][0])
	assert.Equal(t, "#1", rows[0][2])
	assert.Equal(t, "behind", rows[0][5])
	assert.Equal(t, testAuthor, rows[0][6])
}

func TestUpdate_WindowSize(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.Equal(t, 90, mm.width)
	assert.Nil(t, cmd)
}

func TestUpdate_SearchDone(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	updated, _ := m.Update(searchDoneMsg{prs: testPRs()})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.False(t, mm.loading)
	assert.Equal(t, testPRs(), mm.prs)
}

func TestUpdate_ActionDone(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	updated, cmd := m.Update(actionDoneMsg{})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.Equal(t, screenResults, mm.screen)
	assert.Nil(t, cmd)
}

func TestUpdate_ActionProgress(t *testing.T) {
	t.Parallel()

	t.Run("reschedules while running", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenResults
		m.results = nil

		_, cmd := m.Update(actionProgressMsg{})
		require.NotNil(t, cmd)
	})

	t.Run("stops once results land", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenResults
		m.results = []worker.Result{}

		_, cmd := m.Update(actionProgressMsg{})
		assert.Nil(t, cmd)
	})
}

func TestUpdate_SpinnerTick(t *testing.T) {
	t.Parallel()

	tick := spinner.TickMsg{}

	t.Run("active while loading", func(t *testing.T) {
		t.Parallel()

		m := New(nil, "q") // loading: true
		_, cmd := m.Update(tick)
		assert.NotNil(t, cmd)
	})

	t.Run("inactive once loaded and idle", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		_, cmd := m.Update(tick)
		assert.Nil(t, cmd)
	})
}

func TestUpdate_KeyMsgDispatch(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	updated, _ := m.Update(keyMsgFromString("/"))

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.Equal(t, screenFilter, mm.screen)
}

func TestUpdate_UnknownMsg(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	updated, cmd := m.Update(struct{}{})

	assert.Equal(t, m, updated)
	assert.Nil(t, cmd)
}

func TestHandleKey_AllScreens(t *testing.T) {
	t.Parallel()

	tests := map[string]screen{
		"list":         screenList,
		"filter":       screenFilter,
		"action input": screenActionInput,
		"confirm":      screenConfirm,
		"results":      screenResults,
	}

	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.screen = s
			m.action = &pendingAction{label: actionClose, run: noopAction}
			m.results = []worker.Result{}

			// esc is handled distinctly (or ignored) by every screen without panicking.
			assert.NotPanics(t, func() { m.handleKey(keyMsgFromString("esc")) })
		})
	}
}

func TestHandleListKey_DefaultPassesToTable(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	before := m.table.Cursor()

	m, _ = m.handleListKeyByString("j")
	assert.Greater(t, m.table.Cursor(), before, "j should move the table cursor down")
}

func TestEnhanceCommand(t *testing.T) {
	t.Parallel()

	cmd := enhanceCommand(github.PR{URL: "https://github.com/hugoh/a/pull/1"})

	assert.Equal(t, []string{"gh", "enhance", "https://github.com/hugoh/a/pull/1"}, cmd.Args)
}

func TestHandleListKey_EnhanceLaunchesForFocusedPR(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	_, cmd := m.handleListKeyByString("T")

	assert.NotNil(t, cmd)
}

func TestHandleListKey_EnhanceNoopWithoutPRs(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	_, cmd := m.handleListKeyByString("T")

	assert.Nil(t, cmd)
}

func TestHandleListKey_ToggleSelection_SameNumberInDifferentRepos(t *testing.T) {
	t.Parallel()

	m := New(nil, "is:open is:pr")
	m = m.handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.handleSearchDone(searchDoneMsg{prs: []github.PR{
		{Number: 7, Title: "First", Repo: testRepoA, Author: testAuthor},
		{Number: 7, Title: "Second", Repo: testRepoB, Author: testAuthor},
	}})

	m, _ = m.handleListKeyByString("x")

	got := m.selectedPRs()
	require.Len(t, got, 1)
	assert.Equal(t, testRepoA, got[0].Repo)
}

func TestTabs(t *testing.T) {
	t.Parallel()

	t.Run("starting on a tab query selects it", func(t *testing.T) {
		t.Parallel()

		m := New(nil, tabs()[1].query)
		assert.Equal(t, 1, m.tab)
	})

	t.Run("custom query selects no tab", func(t *testing.T) {
		t.Parallel()

		m := New(nil, "is:open is:pr author:hugoh")
		assert.Equal(t, noTab, m.tab)
	})

	t.Run("1 and 2 switch tab and rerun the search", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()

		m, cmd := m.handleListKeyByString("2")
		assert.Equal(t, 1, m.tab)
		assert.Equal(t, "is:open is:pr archived:false sort:updated-desc owner:@me", m.query)
		assert.True(t, m.loading)
		require.NotNil(t, cmd)

		m, _ = m.handleListKeyByString("1")
		assert.Equal(t, 0, m.tab)
		assert.Equal(t, "is:open is:pr archived:false sort:updated-desc involves:@me", m.query)
	})

	t.Run("/ edits the full current query", func(t *testing.T) {
		t.Parallel()

		m, _ := loadedModel().handleListKeyByString("2")
		m, _ = m.handleListKeyByString("/")

		assert.Equal(t, screenFilter, m.screen)
		assert.Equal(t, m.query, m.filterInput.Value())
	})

	t.Run("filter that differs from every tab deselects", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenFilter
		m.filterInput.SetValue("is:open is:pr author:hugoh")

		m, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})
		assert.Equal(t, noTab, m.tab)
	})

	t.Run("filter that matches a tab selects it", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenFilter
		m.filterInput.SetValue("is:open  is:pr archived:false sort:updated-desc owner:@me")

		m, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})
		assert.Equal(t, 1, m.tab)
	})

	t.Run("view shows the tab names", func(t *testing.T) {
		t.Parallel()

		view := loadedModel().View()
		assert.Contains(t, view, "involves:@me")
		assert.Contains(t, view, "owner:@me")
	})
}

func TestQueryChangesAreRecorded(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	m := loadedModel().WithHistory(history.New(path))

	m, _ = m.handleListKeyByString("2")
	m.screen = screenFilter
	m.filterInput.SetValue("is:open is:pr author:hugoh")
	_, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})

	got, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	require.NoError(t, err)
	assert.Equal(
		t,
		"is:open is:pr archived:false sort:updated-desc owner:@me\nis:open is:pr author:hugoh\n",
		string(got),
	)
}

func reloaded(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()

	return m.reload()
}

func TestSearchIgnoresStaleResults(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = reloaded(t, m)
	firstID := m.searchID
	m, _ = reloaded(t, m)
	require.Greater(t, m.searchID, firstID)

	m = m.handleSearchDone(
		searchDoneMsg{id: firstID, prs: []github.PR{{Number: 99, Repo: testRepoA}}},
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

func TestTabSwitchUsesCache(t *testing.T) {
	t.Parallel()

	m := New(nil, tabs()[0].query)
	m = m.handleSearchDone(searchDoneMsg{query: tabs()[0].query, prs: testPRs()})

	m, _ = m.handleListKeyByString("2")
	assert.Empty(t, m.prs, "uncached tab starts empty rather than showing another query's rows")
	assert.Empty(t, m.table.Rows())

	other := []github.PR{{Number: 5, Title: "Mine", Repo: testRepoA, Author: testAuthor}}
	m = m.handleSearchDone(searchDoneMsg{id: m.searchID, query: tabs()[1].query, prs: other})

	m, cmd := m.handleListKeyByString("1")
	assert.Equal(t, testPRs(), m.prs, "cached rows show immediately")
	assert.Len(t, m.table.Rows(), 2)
	assert.True(t, m.loading, "cached tab still refreshes in the background")
	require.NotNil(t, cmd)
}

func TestRefreshKeepsSelectionOfSurvivingPRs(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true
	m.selected[keyOf(m.prs[1])] = true

	m = m.handleSearchDone(searchDoneMsg{prs: testPRs()[:1]})

	assert.Equal(t, map[prKey]bool{keyOf(testPRs()[0]): true}, m.selected)
}

func TestTabSwitchClearsSelection(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true

	m, _ = m.handleListKeyByString("2")

	assert.Empty(t, m.selected)
}

func TestFinishingActionForcesFullReload(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.cache[m.query] = results{prs: m.prs}
	m.screen = screenResults
	m.results = []worker.Result{{}}

	m, _ = m.handleResultsKey(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Empty(t, m.prs, "acted-on PRs must not linger as stale rows")
	assert.Empty(t, m.selected)
	assert.NotContains(t, m.cache, m.query)
	assert.True(t, m.loading)
}

func TestLightSearchThenFull(t *testing.T) {
	t.Parallel()

	light := []github.PR{{Number: 1, Title: "Fix bug", Repo: testRepoA, Author: testAuthor}}

	m := New(nil, "q")

	m = m.handleSearchDone(searchDoneMsg{light: true, prs: light})
	assert.Equal(t, light, m.prs, "light result paints the list")
	assert.True(t, m.loading, "still waiting for the full result")
	assert.Equal(t, "…", m.table.Rows()[0][4], "checks column is a placeholder until details land")
	assert.Equal(t, "…", m.table.Rows()[0][5], "merge column is a placeholder until details land")

	m = m.handleSearchDone(searchDoneMsg{prs: testPRs()})
	assert.Equal(t, testPRs(), m.prs)
	assert.False(t, m.loading)

	m = m.handleSearchDone(searchDoneMsg{light: true, prs: light})
	assert.Equal(t, testPRs(), m.prs, "late light result must not clobber the full one")
}

func TestLightSearchErrorIsIgnored(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	m = m.handleSearchDone(searchDoneMsg{light: true, err: assert.AnError})

	require.NoError(t, m.err)
	assert.True(t, m.loading)
}

func TestReloadKeepsRows(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = reloaded(t, m)

	assert.Equal(t, testPRs(), m.prs, "refresh keeps showing the current rows")
}

// manyPRs builds count PRs numbered from first, all in testRepoA.
func manyPRs(first, count int, detailed bool) []github.PR {
	prs := make([]github.PR, count)
	for i := range prs {
		prs[i] = github.PR{
			Number:   first + i,
			Title:    "PR",
			Repo:     testRepoA,
			Author:   testAuthor,
			Detailed: detailed,
		}
		if detailed {
			prs[i].Checks = github.ChecksPass
		}
	}

	return prs
}

// pagedModel is a loaded first page of 50 out of 312 matches.
func pagedModel() Model {
	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m.handleSearchDone(searchDoneMsg{
		query: "q", prs: manyPRs(1, 50, true), total: 312, cursor: "c1", hasNext: true,
	})
}

func TestFullPageStoresPaging(t *testing.T) {
	t.Parallel()

	m := pagedModel()

	assert.Equal(t, 312, m.total)
	assert.Equal(t, "c1", m.endCursor)
	assert.True(t, m.hasMore)
	assert.Equal(t, results{prs: m.prs, total: 312, cursor: "c1", hasMore: true}, m.cache["q"])
}

func TestLightPageSetsTotalOnly(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").handleSearchDone(searchDoneMsg{
		light: true, prs: manyPRs(1, 3, false), total: 312, cursor: "c1", hasNext: true,
	})

	assert.Equal(t, 312, m.total)
	assert.False(t, m.hasMore, "only the full result decides whether more can be loaded")
	assert.Empty(t, m.endCursor)
	assert.NotContains(t, m.cache, "q", "light rows are never cached")
}

func TestLightNeverOverwritesDetailedRow(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m = m.handleSearchDone(searchDoneMsg{light: true, prs: manyPRs(1, 1, false)})

	assert.True(t, m.prs[0].Detailed)
	assert.Equal(t, github.ChecksPass, m.prs[0].Checks)
	assert.Len(t, m.prs, 50)
}

func TestLoadMore_LightThenFull(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(
		searchDoneMsg{light: true, more: true, prs: manyPRs(51, 50, false), total: 312},
	)
	assert.Len(t, m.prs, 100, "light rows are appended right away")
	assert.Equal(t, "…", m.table.Rows()[50][4])
	assert.True(t, m.loadingMore, "still waiting for the full page")

	m = m.handleSearchDone(
		searchDoneMsg{query: "q", more: true, prs: manyPRs(51, 50, true), total: 312, cursor: "c2"},
	)
	assert.Len(t, m.prs, 100, "full rows replace the light ones in place")
	assert.True(t, m.prs[99].Detailed)
	assert.NotEqual(t, "…", m.table.Rows()[50][4])
	assert.False(t, m.loadingMore)
	assert.Equal(t, "c2", m.endCursor)
	assert.False(t, m.hasMore)
	assert.Len(t, m.cache["q"].prs, 100)
	assert.Equal(t, "c2", m.cache["q"].cursor)
}

func TestLoadMore_FullBeforeLight(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(
		searchDoneMsg{more: true, prs: manyPRs(51, 50, true), cursor: "c2", hasNext: true},
	)
	m = m.handleSearchDone(searchDoneMsg{light: true, more: true, prs: manyPRs(51, 50, false)})

	assert.Len(t, m.prs, 100)
	assert.True(t, m.prs[99].Detailed, "late light page must not downgrade rows")
}

func TestLoadMore_DedupesShiftedResults(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{more: true, prs: manyPRs(50, 50, true)})

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
	assert.Contains(t, m.View(), "load more failed")
}

func TestLoadMore_StaleResultDroppedAfterReload(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true
	staleID := m.searchID

	m, _ = reloaded(t, m)
	assert.False(t, m.loadingMore, "a new search abandons the in-flight page")

	m = m.handleSearchDone(searchDoneMsg{id: staleID, more: true, prs: manyPRs(51, 50, true)})
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

func TestMovingNearBottomLoadsMore(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.table.SetCursor(50 - loadMoreMargin - 1)

	m, cmd := m.handleListKeyByString("j")

	assert.True(t, m.loadingMore)
	assert.NotNil(t, cmd)
}

func TestTabSwitchRestoresPaging(t *testing.T) {
	t.Parallel()

	first, second := tabs()[0].query, tabs()[1].query

	m := New(nil, first)
	m = m.handleSearchDone(
		searchDoneMsg{
			query:   first,
			prs:     manyPRs(1, 50, true),
			total:   312,
			cursor:  "c1",
			hasNext: true,
		},
	)

	m, _ = m.handleListKeyByString("2")
	assert.Equal(t, second, m.query)
	assert.Zero(t, m.total)
	assert.Empty(t, m.endCursor)
	assert.False(t, m.hasMore)

	m, _ = m.handleListKeyByString("1")
	assert.Equal(t, 312, m.total)
	assert.Equal(t, "c1", m.endCursor)
	assert.True(t, m.hasMore)
}

func TestFilterHistoryRecall(t *testing.T) {
	t.Parallel()

	log := history.New(filepath.Join(t.TempDir(), "history"))
	for _, query := range []string{"first", "second", "third"} {
		require.NoError(t, log.Add(query))
	}

	m := loadedModel().WithHistory(log)
	m, _ = m.handleListKeyByString("/")
	m.filterInput.SetValue("draft")

	press := func(m Model, keyType tea.KeyType) Model {
		m, _ = m.handleFilterKey(tea.KeyMsg{Type: keyType})

		return m
	}

	m = press(m, tea.KeyUp)
	assert.Equal(t, "third", m.filterInput.Value())
	m = press(m, tea.KeyUp)
	m = press(m, tea.KeyUp)
	assert.Equal(t, "first", m.filterInput.Value())
	m = press(m, tea.KeyUp)
	assert.Equal(t, "first", m.filterInput.Value(), "stops at the oldest entry")

	m = press(m, tea.KeyDown)
	assert.Equal(t, "second", m.filterInput.Value())
	m = press(m, tea.KeyDown)
	m = press(m, tea.KeyDown)
	assert.Equal(t, "draft", m.filterInput.Value(), "going past the newest restores what was typed")
	m = press(m, tea.KeyDown)
	assert.Equal(t, "draft", m.filterInput.Value())
}

func TestFilterHistoryRecall_NoHistory(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().handleListKeyByString("/")
	m, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyUp})

	assert.Equal(t, m.query, m.filterInput.Value())
}

func TestStartFilterPutsCursorAtEnd(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.filterInput.SetValue("abc")
	m.filterInput.SetCursor(1)

	m, _ = m.handleListKeyByString("/")

	assert.Equal(t, len(m.query), m.filterInput.Position())
}

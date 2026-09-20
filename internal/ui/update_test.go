package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	textLabel   = "label"
	textFilter  = "filter"
	textHelp    = "help"
	textShort   = "short"
	actionClose = "close"
	actionMerge = "merge"
	textAuto    = "auto"
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

// keyMsgFromString builds a tea.KeyPressMsg whose String() matches s, for the
// handful of key forms this app switches on ("esc", "enter", "ctrl+a",
// "ctrl+c", "space", or a single printable rune like "x" or "/").
func keyMsgFromString(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case " ", "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
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
		textFilter:     screenFilter,
		"action input": screenActionInput,
		"confirm":      screenConfirm,
		"results":      screenResults,
		textHelp:       screenHelp,
	}

	for name, current := range screens {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.screen = current
			m.action = &pendingAction{label: actionClose, run: noopAction}
			m.results = nil // a bulk action still running

			_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
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

		m, cmd := m.handleFilterKey(tea.KeyPressMsg{Code: tea.KeyEnter})

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

		m, _ = m.handleFilterKey(tea.KeyPressMsg{Code: tea.KeyEsc})

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

		m, _ = m.handleActionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})

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

		m, _ = m.handleActionInputKey(tea.KeyPressMsg{Code: tea.KeyEsc})

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

		m, cmd := m.handleConfirmKey(tea.KeyPressMsg{Code: tea.KeyEnter})

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

			same, cmd := m.handleConfirmKey(tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestListKey_AutoMerge(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cleanPRs        []int // indexes into the two loaded PRs that are already clean
		wantDestructive bool
		wantMergesNow   int
	}{
		"none clean stays easy": {},
		"one clean needs an explicit yes": {
			cleanPRs:        []int{1},
			wantDestructive: true,
			wantMergesNow:   1,
		},
		"all clean": {
			cleanPRs:        []int{0, 1},
			wantDestructive: true,
			wantMergesNow:   2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			for _, idx := range tt.cleanPRs {
				m.prs[idx].MergeState = mergeClean
			}

			for _, pull := range m.prs {
				m.selected[keyOf(pull)] = true
			}

			m, _ = m.handleListKeyByString("a")

			require.Equal(t, screenConfirm, m.screen)
			require.NotNil(t, m.action)
			assert.Equal(t, "toggle auto-merge", m.action.label)
			assert.Equal(t, tt.wantDestructive, m.action.destructive)
			assert.Equal(t, tt.wantMergesNow, strings.Count(m.confirmBody(), "merges now"))
		})
	}
}

func TestHandleResultsKey(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenResults
	m.results = []worker.Result{}

	m, cmd := m.handleResultsKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, screenList, m.screen)
	assert.True(t, m.loading)
	require.NotNil(t, cmd)
}

func TestHandleResultsKey_IgnoredWhileRunning(t *testing.T) {
	t.Parallel()

	for _, keyType := range []rune{tea.KeyEnter, tea.KeyEsc} {
		m := loadedModel()
		m.screen = screenResults
		m.results = nil

		m, cmd := m.handleResultsKey(tea.KeyPressMsg{Code: keyType})

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
	assert.Empty(t, rows[0][6], "no auto-merge cell when it is off")
	assert.Equal(t, testAuthor, rows[0][7])
}

func TestRowsFor_ShowsAutoMerge(t *testing.T) {
	t.Parallel()

	light := github.PR{Number: 1, AutoMerge: true}
	rows := rowsFor([]github.PR{light}, nil)

	assert.Equal(t, "on", rows[0][6], "auto-merge comes with the light search, before details land")
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

func TestUpdate_ActionDone_AppliesOnDoneToSucceededPRs(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.action = &pendingAction{
		label:  "toggle auto-merge",
		onDone: func(pr *github.PR) { pr.AutoMerge = !pr.AutoMerge },
	}

	prs := testPRs()
	updated, _ := m.Update(actionDoneMsg{results: []worker.Result{
		{PR: prs[0]},
		{PR: prs[1], Err: errors.New("boom")},
	}})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.True(t, mm.prs[0].AutoMerge)
	assert.False(t, mm.prs[1].AutoMerge, "a failed PR keeps its state")
	assert.Equal(t, "on", mm.table.Rows()[0][6])
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
	m.open = nil // funcs never compare equal, so the model can't be compared with one set

	updated, cmd := m.Update(struct{}{})

	// bubbles v2's viewport.Model (embedded in both the table and the confirm
	// pane) carries a non-nil default gutter func, so reflect.DeepEqual (which
	// assert.Equal uses) can never call two of them equal, even when they're
	// the very same value copied by value. Compare formatted representations
	// instead, which render identically for identical values.
	assert.Equal(t, fmt.Sprintf("%#v", m), fmt.Sprintf("%#v", updated))
	assert.Nil(t, cmd)
}

func TestHandleKey_AllScreens(t *testing.T) {
	t.Parallel()

	tests := map[string]screen{
		"list":         screenList,
		textFilter:     screenFilter,
		"action input": screenActionInput,
		"confirm":      screenConfirm,
		"results":      screenResults,
		textHelp:       screenHelp,
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

		m, _ = m.handleFilterKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		assert.Equal(t, noTab, m.tab)
	})

	t.Run("filter that matches a tab selects it", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenFilter
		m.filterInput.SetValue("is:open  is:pr archived:false sort:updated-desc owner:@me")

		m, _ = m.handleFilterKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		assert.Equal(t, 1, m.tab)
	})

	t.Run("view shows the tab names", func(t *testing.T) {
		t.Parallel()

		view := loadedModel().View().Content
		assert.Contains(t, view, "involves:@me")
		assert.Contains(t, view, "owner:@me")
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

	record(tabs()[1].query)
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

	m, _ = m.handleResultsKey(tea.KeyPressMsg{Code: tea.KeyEnter})

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

func lightNextPageModel(t *testing.T) Model {
	t.Helper()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{light: true, more: true, prs: manyPRs(51, 50, false)})
	require.Len(t, m.prs, 100)

	return m
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
	assert.Contains(t, m.View().Content, "load more failed")
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

// openFilter presses / and delivers the history the command loads.
func openFilter(t *testing.T, m Model) Model {
	t.Helper()

	m, cmd := m.handleListKeyByString("/")
	require.NotNil(t, cmd, "opening the filter loads the history in a command")

	msg := cmd()
	require.IsType(t, historyLoadedMsg{}, msg)

	updated, _ := m.Update(msg)

	opened, ok := updated.(Model)
	require.True(t, ok)

	return opened
}

func TestFilterHistoryRecall(t *testing.T) {
	t.Parallel()

	log := history.New(filepath.Join(t.TempDir(), "history"))
	for _, query := range []string{"first", "second", "third"} {
		require.NoError(t, log.Add(query))
	}

	m := openFilter(t, loadedModel().WithHistory(log))
	m.filterInput.SetValue("draft")

	press := func(m Model, keyType rune) Model {
		m, _ = m.handleFilterKey(tea.KeyPressMsg{Code: keyType})

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

func TestOpeningFilterDoesNoFileIO(t *testing.T) {
	t.Parallel()

	log := history.New(filepath.Join(t.TempDir(), "history"))
	require.NoError(t, log.Add("first"))

	m, _ := loadedModel().WithHistory(log).handleListKeyByString("/")

	assert.Empty(
		t,
		m.filterHistory,
		"history arrives with the command's message, not inside Update",
	)
}

func TestHistoryLoadedIsIgnoredOutsideTheFilter(t *testing.T) {
	t.Parallel()

	updated, _ := loadedModel().Update(historyLoadedMsg{entries: []string{"a"}})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.Empty(t, mm.filterHistory)
}

func TestFilterHistoryRecall_NoHistory(t *testing.T) {
	t.Parallel()

	m, cmd := loadedModel().handleListKeyByString("/")
	assert.Nil(t, cmd, "nothing to load without a history log")

	m, _ = m.handleFilterKey(tea.KeyPressMsg{Code: tea.KeyUp})

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

func TestSyncTableHeight_NeverNegative(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: 3})

	assert.GreaterOrEqual(t, m.table.Height(), 0)
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

	m = m.handleSearchDone(searchDoneMsg{query: "q", more: true, prs: manyPRs(51, 1, true)})

	assert.True(t, released)
	assert.Nil(t, m.cancelMore)
}

func TestLoadMore_FullDropsLightRowsItDidNotReturn(t *testing.T) {
	t.Parallel()

	m := lightNextPageModel(t)

	// PR 100 was updated between the two calls and slid off this page; 101 slid on.
	full := append(manyPRs(51, 49, true), manyPRs(101, 1, true)...)
	m = m.handleSearchDone(
		searchDoneMsg{query: "q", more: true, prs: full, cursor: "c2", hasNext: true},
	)

	assert.Len(t, m.prs, 100)

	for _, pr := range m.prs {
		assert.True(t, pr.Detailed, "PR %d must not be left as a placeholder row", pr.Number)
		assert.NotEqual(t, 100, pr.Number)
	}

	assert.Equal(t, 101, m.prs[99].Number)
}

func TestLoadMore_LightAfterFullIsIgnored(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	m = m.handleSearchDone(searchDoneMsg{query: "q", more: true, prs: manyPRs(51, 10, true)})
	m = m.handleSearchDone(searchDoneMsg{light: true, more: true, prs: manyPRs(51, 12, false)})

	assert.Len(t, m.prs, 60, "the light page has nothing left to add once the full one landed")
}

func TestLightPageOneAfterFullIsIgnored(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m = m.handleSearchDone(searchDoneMsg{light: true, prs: manyPRs(1, 51, false)})

	assert.Len(t, m.prs, 50)
}

func TestLoadMore_FailedPageDropsItsPlaceholders(t *testing.T) {
	t.Parallel()

	m := lightNextPageModel(t)
	m.selected[keyOf(m.prs[75])] = true

	m = m.handleSearchDone(searchDoneMsg{more: true, err: assert.AnError})

	assert.Len(t, m.prs, 50, "the failed page's rows go; a retry fetches them again")
	assert.Empty(t, m.selected, "selection can't point at rows that are gone")
	require.ErrorIs(t, m.moreErr, assert.AnError)
	assert.Len(t, m.table.Rows(), 50)
}

func TestQuitWithASelectionNeedsASecondPress(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true

	armed, cmd := m.handleListKeyByString("q")
	assert.Nil(t, cmd, "the first q only asks")
	assert.True(t, armed.quitArmed)
	assert.Contains(t, armed.footerText(), "q again to quit")
	assert.Contains(t, armed.footerText(), "1 selected")

	_, cmd = armed.handleListKeyByString("q")
	require.NotNil(t, cmd)

	_, ok := cmd().(tea.QuitMsg)
	assert.True(t, ok, "the second q quits")
}

func TestQuitConfirmationIsCancelledByAnyOtherKey(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true

	armed, _ := m.handleListKeyByString("q")
	moved, _ := armed.handleListKeyByString("j")
	assert.False(t, moved.quitArmed)
	assert.NotContains(t, moved.footerText(), "q again")

	again, cmd := moved.handleListKeyByString("q")
	assert.Nil(t, cmd, "after another key the first q asks again")
	assert.True(t, again.quitArmed)
}

func TestQuitWithoutASelectionIsImmediate(t *testing.T) {
	t.Parallel()

	_, cmd := loadedModel().handleListKeyByString("q")
	require.NotNil(t, cmd)

	_, ok := cmd().(tea.QuitMsg)
	assert.True(t, ok)
}

func TestCtrlCQuitsEvenWithASelection(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.selected[keyOf(m.prs[0])] = true

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)

	_, ok := cmd().(tea.QuitMsg)
	assert.True(t, ok)
}

func wheel(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: button}
}

func TestMouseWheelMovesTheCursor(t *testing.T) {
	t.Parallel()

	m := pagedModel()

	updated, _ := m.Update(wheel(tea.MouseWheelDown))
	down, ok := updated.(Model)
	require.True(t, ok)
	assert.Equal(t, wheelStep, down.table.Cursor())

	updated, _ = down.Update(wheel(tea.MouseWheelUp))
	up, ok := updated.(Model)
	require.True(t, ok)
	assert.Zero(t, up.table.Cursor())
}

func TestMouseWheelNearTheBottomLoadsMore(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.table.SetCursor(50 - loadMoreMargin - wheelStep)

	updated, cmd := m.Update(wheel(tea.MouseWheelDown))
	scrolled, ok := updated.(Model)
	require.True(t, ok)

	assert.True(t, scrolled.loadingMore)
	assert.NotNil(t, cmd)
}

func TestMouseIgnoresEverythingButWheelPresses(t *testing.T) {
	t.Parallel()

	for name, msg := range map[string]tea.MouseMsg{
		"left click":    tea.MouseClickMsg{Button: tea.MouseLeft},
		"wheel release": tea.MouseReleaseMsg{Button: tea.MouseWheelDown},
		"motion":        tea.MouseMotionMsg{Button: tea.MouseWheelDown},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			updated, cmd := pagedModel().Update(msg)
			unchanged, ok := updated.(Model)
			require.True(t, ok)

			assert.Zero(t, unchanged.table.Cursor())
			assert.Nil(t, cmd)
		})
	}
}

func TestMouseWheelScrollsTheConfirmList(t *testing.T) {
	t.Parallel()

	updated, _ := bigConfirmModel().Update(wheel(tea.MouseWheelDown))
	scrolled, ok := updated.(Model)
	require.True(t, ok)

	assert.Positive(t, scrolled.pane.YOffset())
	assert.Equal(t, screenConfirm, scrolled.screen)
}

func TestMouseWheelIsIgnoredWhilePrompting(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.screen = screenFilter

	updated, _ := m.Update(wheel(tea.MouseWheelDown))
	same, ok := updated.(Model)
	require.True(t, ok)

	assert.Zero(t, same.table.Cursor())
}

const testPRURL = "https://github.com/hugoh/a/pull/"

// browserModel is a 50-PR model whose PRs have URLs and whose browser records
// every URL it is asked to open (failing with openErr if set). The first
// selected rows are selected.
func browserModel(selected int, opened *[]string, openErr error) Model {
	m := pagedModel()

	for i := range m.prs {
		m.prs[i].URL = fmt.Sprintf("%s%d", testPRURL, m.prs[i].Number)
	}

	m.open = func(url string) error {
		*opened = append(*opened, url)

		return openErr
	}

	for i := range selected {
		m.selected[keyOf(m.prs[i])] = true
	}

	return m
}

// pressAll presses the keys in order, running each command they return and
// feeding its message back, like the bubbletea runtime would.
func pressAll(t *testing.T, m Model, keys ...string) Model {
	t.Helper()

	for _, pressed := range keys {
		var cmd tea.Cmd

		m, cmd = m.handleListKeyByString(pressed)
		if cmd == nil {
			continue
		}

		if msg := cmd(); msg != nil {
			updated, _ := m.Update(msg)

			var ok bool

			m, ok = updated.(Model)
			require.True(t, ok)
		}
	}

	return m
}

func urls(numbers ...int) []string {
	out := make([]string, len(numbers))
	for i, number := range numbers {
		out[i] = fmt.Sprintf("%s%d", testPRURL, number)
	}

	return out
}

func TestOpenInBrowser(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cursor  int
		noURL   bool
		noRows  bool
		want    []string
		wantNil bool // the key returns no command at all
	}{
		"opens the PR under the cursor": {cursor: 0, want: urls(1)},
		"follows the cursor":            {cursor: 2, want: urls(3)},
		"a PR without a URL":            {noURL: true, wantNil: true},
		"no rows":                       {noRows: true, wantNil: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var opened []string

			m := browserModel(0, &opened, nil)
			if tt.noRows {
				m = m.withResults(results{}).refreshRows()
			}

			m.table.SetCursor(tt.cursor)

			if tt.noURL {
				m.prs[0].URL = ""
			}

			after, cmd := m.handleListKeyByString("o")
			require.Equal(t, tt.wantNil, cmd == nil)

			if cmd != nil {
				require.Nil(t, cmd(), "success needs no message")
			}

			require.Equal(t, tt.want, opened)
			require.Empty(t, after.selected, "opening doesn't select")
			require.False(t, after.previewOpen)
		})
	}
}

func TestOpenSelected(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		selected   int
		noURL      int // index of a selected PR to strip the URL from, or -1
		keys       []string
		want       []string
		wantArmed  bool
		wantNotice string
		wantFooter string
	}{
		"up to the limit opens at once": {
			selected: openAllConfirmAbove, noURL: -1, keys: []string{"O"},
			want: urls(1, 2, 3, 4, 5),
		},
		"more asks first": {
			selected: 8, noURL: -1, keys: []string{"O"},
			wantArmed: true, wantFooter: "O again to open all 8",
		},
		"the second press opens them": {
			selected: 8, noURL: -1, keys: []string{"O", "O"},
			want: urls(1, 2, 3, 4, 5, 6, 7, 8),
		},
		"another key cancels the question": {
			selected: 8, noURL: -1, keys: []string{"O", "j"},
		},
		"and it asks again afterwards": {
			selected: 8, noURL: -1, keys: []string{"O", "j", "O"},
			wantArmed: true,
		},
		"too many is refused": {
			selected: openAllMax + 1, noURL: -1, keys: []string{"O", "O"},
			wantNotice: "O can open at most 20",
		},
		"the maximum still asks": {
			selected: openAllMax, noURL: -1, keys: []string{"O"},
			wantArmed: true,
		},
		"nothing selected": {selected: 0, noURL: -1, keys: []string{"O"}},
		"PRs without a URL are skipped": {
			selected: 3, noURL: 1, keys: []string{"O"},
			want: urls(1, 3),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var opened []string

			m := browserModel(tt.selected, &opened, nil)
			if tt.noURL >= 0 {
				m.prs[tt.noURL].URL = ""
			}

			after := pressAll(t, m, tt.keys...)

			require.Equal(t, tt.want, opened)
			require.Equal(t, tt.wantArmed, after.openArmed)

			if tt.wantNotice != "" {
				require.Contains(t, after.statusLine(), tt.wantNotice)
			}

			if tt.wantFooter != "" {
				require.Contains(t, after.footerText(), tt.wantFooter)
			}
		})
	}
}

func TestOpenFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		selected  int
		keys      []string
		wantCalls int
	}{
		"one PR":                            {selected: 0, keys: []string{"o"}, wantCalls: 1},
		"one failure doesn't stop the rest": {selected: 3, keys: []string{"O"}, wantCalls: 3},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var opened []string

			m := browserModel(tt.selected, &opened, errors.New("no browser"))

			failed := pressAll(t, m, tt.keys...)

			require.Len(t, opened, tt.wantCalls)
			require.Contains(t, failed.statusLine(), "couldn't open PR")
			require.Contains(t, failed.statusLine(), "no browser")

			cleared := pressAll(t, failed, "j")
			require.NotContains(t, cleared.statusLine(), "couldn't open")
		})
	}
}

func TestNew_HasARealBrowserByDefault(t *testing.T) {
	t.Parallel()

	require.NotNil(t, New(nil, "q").open)
}

package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsForKey(t *testing.T) {
	t.Parallel()

	client := &github.Client{}

	tests := map[string]struct {
		key       string
		input     string
		wantLabel string
	}{
		textLabel:   {"l", "bug", `add label "bug"`},
		actionClose: {"c", "", actionClose},
		actionMerge: {"m", "", actionMerge},
		textAuto:    {"a", "", "toggle auto-merge"},
		"unknown":   {"z", "", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			action := actionsForKey(client, tt.key, tt.input)

			if tt.wantLabel == "" {
				assert.Nil(t, action)

				return
			}

			require.NotNil(t, action)
			assert.Equal(t, tt.wantLabel, action.label)
			assert.NotNil(t, action.run)
		})
	}
}

func TestPollActionProgress(t *testing.T) {
	t.Parallel()

	cmd := pollActionProgress()
	require.NotNil(t, cmd)

	msg := cmd()
	_, ok := msg.(actionProgressMsg)
	assert.True(t, ok)
}

func TestActionsForKey_Destructive(t *testing.T) {
	t.Parallel()

	client := &github.Client{}

	assert.False(t, actionsForKey(client, "l", "bug").destructive, "labelling is easy to undo")
	assert.True(t, actionsForKey(client, "c", "").destructive)
	assert.True(t, actionsForKey(client, "m", "").destructive)
	assert.False(
		t,
		actionsForKey(client, "a", "").destructive,
		"toggling auto-merge is easy to undo",
	)
}

func TestActionsForKey_AutoMergeLeavesAMergedPRAlone(t *testing.T) {
	t.Parallel()

	action := actionsForKey(&github.Client{}, "a", "")
	pull := github.PR{MergeState: github.MergeClean}

	action.onDone(&pull)
	assert.False(t, pull.AutoMerge, "a clean PR was merged, not given auto-merge")
}

func TestActionsForKey_AutoMergeFlipsLocalState(t *testing.T) {
	t.Parallel()

	action := actionsForKey(&github.Client{}, "a", "")
	require.NotNil(t, action.onDone)

	pr := github.PR{AutoMerge: false}
	action.onDone(&pr)
	assert.True(t, pr.AutoMerge)

	action.onDone(&pr)
	assert.False(t, pr.AutoMerge)
}

func TestStartAction_AutoMergeNeedsLoadedDetails(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.prs[0].Detailed = false

	m, _ = m.handleListKeyByString("a")
	assert.Equal(t, screenList, m.screen)
	assert.NotEmpty(t, m.notice)
}

func TestHandleListKey_StartAction_NoSelectionTargetsFocused(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("j")
	m, _ = m.handleListKeyByString("c")

	assert.Equal(t, screenConfirm, m.screen)
	assert.Equal(
		t,
		[]github.PR{m.prs[1]},
		m.confirm,
		"with nothing selected, actions apply to the focused PR",
	)
	assert.Empty(t, m.selected)
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

	t.Run("enter with a blank label stays on the input", func(t *testing.T) {
		t.Parallel()

		m := loadedModel()
		m.screen = screenActionInput
		m.actionKey = "l"
		m.actionInput.SetValue("  ")

		m, _ = m.handleActionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})

		require.Equal(t, screenActionInput, m.screen)
		require.Nil(t, m.action)
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
				m.prs[idx].MergeState = github.MergeClean
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

func TestListKey_UpdateBranch(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().handleListKeyByString("u")

	require.Equal(t, screenConfirm, m.screen)
	require.NotNil(t, m.action)
	assert.Equal(t, "update branch", m.action.label)
	assert.False(t, m.action.destructive)
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

func TestFinishingActionForcesFullReload(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.cache[m.query] = github.Page{PRs: m.prs}
	m.screen = screenResults
	m.results = []worker.Result{{}}

	m, _ = m.handleResultsKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Len(t, m.prs, len(testPRs()), "rows the action didn't touch stay while reloading")
	assert.Empty(t, m.selected)
	assert.NotContains(t, m.cache, m.query)
	assert.True(t, m.loading)
}

func TestApplyOnDone_KeepsStoredDetailsInStep(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.handleDetailsDone(
		detailsDoneMsg{ids: []string{idOf(1)}, details: []github.Detail{detailFor(1)}},
	)
	m.details[idOf(1)] = github.Detail{ID: idOf(1), MergeState: "BLOCKED"}
	m = m.withStoredDetails()
	m.action = actionsForKey(nil, "a", "")

	m = m.applyOnDone([]worker.Result{{PR: m.prs[0]}})
	m = m.withStoredDetails()

	assert.True(t, m.prs[0].AutoMerge, "a later refresh of the rows doesn't undo the toggle")
}

const renovateTestBody = "Update foo.\n\n" +
	" - [ ] <!-- rebase-check -->If you want to rebase/retry this PR, check this box\n"

func TestStartAction_UpdateBranch(t *testing.T) {
	t.Parallel()

	const (
		renovate, dependabot = "renovate", "dependabot"
		update, onGitHub     = "update branch", "update branch (GitHub)"
	)

	tests := []struct {
		key             string
		bot             string
		wantLabel       string
		wantNote        string
		wantDestructive bool
	}{
		{"u", "", update, "", false},
		{"u", renovate, update, "rebase via Renovate", false},
		{"u", dependabot, update, "rebase via Dependabot", false},
		{"U", "", onGitHub, "", false},
		{"U", renovate, onGitHub, "Renovate stops updating this PR", true},
		{"U", dependabot, onGitHub, "Dependabot stops updating this PR", true},
	}

	for _, tt := range tests {
		t.Run(tt.key+" "+tt.bot, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()

			switch tt.bot {
			case renovate:
				m.prs[0].Body = renovateTestBody
			case dependabot:
				m.prs[0].Author = dependabot
			}

			m, _ = m.handleListKeyByString(tt.key)

			require.Equal(t, screenConfirm, m.screen)
			assert.Equal(t, tt.wantLabel, m.action.label)
			assert.Equal(t, tt.wantNote, m.action.note(m.prs[0]))
			assert.Equal(t, tt.wantDestructive, m.action.destructive)
		})
	}
}

func TestStartAction_UpdateNeedsLoadedDetails(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"u", "U"} {
		m := loadedModel()
		m.prs[0].Detailed = false

		m, _ = m.handleListKeyByString(key)

		assert.Equal(t, screenList, m.screen, key)
		assert.NotEmpty(t, m.notice, "whether it is a Renovate PR isn't known yet")
	}
}

func TestStartAction_CloseNotesWhatBotsDo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		author string
		body   string
		want   string
	}{
		{"renovate", "renovate", renovateTestBody, "Renovate ignores this update from now on"},
		{"dependabot", "dependabot", "", "Dependabot skips this version from now on"},
		{"person", testAuthor, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.prs[0].Author, m.prs[0].Body = tt.author, tt.body

			m, _ = m.handleListKeyByString("c")

			require.Equal(t, screenConfirm, m.screen)
			assert.Equal(t, tt.want, m.action.note(m.prs[0]))
			assert.True(t, m.action.destructive)
		})
	}
}

func TestStartAction_CloseNeedsLoadedDetails(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.prs[0].Detailed = false

	m, _ = m.handleListKeyByString("c")

	assert.Equal(t, screenList, m.screen)
	assert.NotEmpty(t, m.notice, "whether it is Renovate's isn't known yet")
}

func TestStartAction_NotesMergesThatDontSquash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key    string
		method string
		want   string
	}{
		{"m", "", ""},
		{"m", github.MergeSquash, ""},
		{"m", github.MergeCommit, "merges with a merge commit"},
		{"m", github.MergeRebase, "merges by rebasing"},
		{"a", github.MergeRebase, "merges now by rebasing"},
	}

	for _, tt := range tests {
		t.Run(tt.key+" "+tt.method, func(t *testing.T) {
			t.Parallel()

			m := loadedModel()
			m.prs[0].MergeMethod, m.prs[0].MergeState = tt.method, github.MergeClean

			m, _ = m.handleListKeyByString(tt.key)

			require.Equal(t, screenConfirm, m.screen)
			assert.Equal(t, tt.want, m.action.note(m.prs[0]))
		})
	}
}

func TestHandleListKey_Approve(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().handleListKeyByString("v")

	require.Equal(t, screenConfirm, m.screen)
	require.NotNil(t, m.action)
	assert.Equal(t, "approve", m.action.label)
	assert.False(t, m.action.destructive)
}

// afterAction is loadedModel with the action for actionKey confirmed.
func afterAction(actionKey string) Model {
	m := loadedModel()
	for i := range m.prs {
		m.prs[i].ID = fmt.Sprintf("PR_%d", m.prs[i].Number)
	}

	m.action = actionsForKey(nil, actionKey, "")

	return m
}

func TestLeavingResultsKeepsRowsOfActionsThatDontClose(t *testing.T) {
	t.Parallel()

	m := afterAction("u")
	m.screen = screenResults
	m.results = []worker.Result{{PR: m.prs[0]}}

	m, _ = m.handleResultsKey(keyMsgFromString("enter"))

	assert.Len(t, m.prs, len(testPRs()))
}

func TestLeavingResultsKeepsMergedPRsOutOfStaleSearches(t *testing.T) {
	t.Parallel()

	m := afterAction("m")
	m.screen = screenResults
	m.results = []worker.Result{{PR: m.prs[0]}}
	stale := slices.Clone(m.prs)

	m, _ = m.handleResultsKey(keyMsgFromString("enter"))
	m = m.handleSearchDone(searchDoneMsg{PRs: stale, Total: 2, id: m.searchID})

	assert.Len(t, m.prs, 1)
	assert.Equal(t, 1, m.total)
}

func TestLeavingResultsKeepsRowsExceptSucceeded(t *testing.T) {
	t.Parallel()

	m := afterAction("m")
	require.GreaterOrEqual(t, len(m.prs), 2)

	done, failed := m.prs[0], m.prs[1]
	m.screen = screenResults
	m.total = 10
	m.results = []worker.Result{{PR: done}, {PR: failed, Err: errors.New("boom")}}

	m, cmd := m.handleResultsKey(keyMsgFromString("enter"))

	assert.NotNil(t, cmd, "a fresh search starts")
	assert.True(t, m.loading)
	assert.Equal(t, screenList, m.screen)
	assert.NotContains(t, m.prs, done)
	assert.Contains(t, m.prs, failed)
	assert.Len(t, m.prs, len(testPRs())-1)
	assert.Equal(t, 9, m.total, "the total drops with the removed rows while reloading")
}

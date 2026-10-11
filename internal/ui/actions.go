package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

type pendingAction struct {
	label       string // human-readable name for the confirm/results screens
	destructive bool   // hard to undo: only an explicit "y" confirms it, never enter
	run         func(ctx context.Context, pr github.PR) error
	onDone      func(pr *github.PR)       // optional: brings a succeeded PR's local copy up to date
	closes      func(pr github.PR) bool   // optional: whether succeeding on pr merges or closes it
	note        func(pr github.PR) string // optional: what this action does to pr, when it isn't obvious
	warns       bool                      // a note is a warning: a PR with one makes the action destructive
}

const progressPollInterval = 150 * time.Millisecond

type actionDoneMsg struct {
	results []worker.Result
}

type actionProgressMsg struct{}

func (m Model) runAction() tea.Cmd {
	action := m.action
	prs := m.confirm
	done := m.actionDone

	return func() tea.Msg {
		results := worker.Run(context.Background(), prs, action.run, done)

		return actionDoneMsg{results: results}
	}
}

// pollActionProgress re-renders the progress bar every tick while a bulk
// action is running; Update stops rescheduling it once results land.
func pollActionProgress() tea.Cmd {
	return tea.Tick(progressPollInterval, func(time.Time) tea.Msg {
		return actionProgressMsg{}
	})
}

func actionsForKey(client *github.Client, actionKey, input string) *pendingAction {
	switch actionKey {
	case "l":
		return &pendingAction{
			label: fmt.Sprintf("add label %q", input),
			run:   func(ctx context.Context, pr github.PR) error { return client.AddLabel(ctx, pr, input) },
		}
	case "c":
		return &pendingAction{
			label:       "close",
			destructive: true,
			run:         client.ClosePR,
			closes:      always,
			note:        closeNote,
		}
	case "m":
		return &pendingAction{
			label:       "merge",
			destructive: true,
			run:         client.MergePR,
			closes:      always,
			note: func(pr github.PR) string {
				if how := mergeHow(pr); how != "" {
					return "merges " + how
				}

				return ""
			},
		}
	case "v":
		return &pendingAction{label: "approve", run: client.ApprovePR}
	case "u", "U":
		return updateAction(client, actionKey == "U")
	case "a":
		return &pendingAction{
			label:  "toggle auto-merge",
			run:    client.ToggleAutoMerge,
			closes: github.PR.MergesNow,
			warns:  true,
			onDone: func(pr *github.PR) {
				if !pr.MergesNow() {
					pr.AutoMerge = !pr.AutoMerge
				}
			},
			note: func(pr github.PR) string {
				if pr.MergesNow() {
					return strings.TrimSpace("merges now " + mergeHow(pr))
				}

				return ""
			},
		}
	}

	return nil
}

func always(github.PR) bool { return true }

// updateAction brings a PR's branch up to date. A bot's PRs are rebased by
// the bot unless onGitHub: GitHub's update commit makes the bot leave the PR
// alone from then on.
func updateAction(client *github.Client, onGitHub bool) *pendingAction {
	if onGitHub {
		return &pendingAction{
			label: "update branch (GitHub)",
			run:   client.UpdateBranch,
			note:  botNote("%s stops updating this PR"),
			warns: true,
		}
	}

	return &pendingAction{
		label: "update branch",
		run: func(ctx context.Context, pr github.PR) error {
			if pr.Bot() != "" {
				return client.RequestRebase(ctx, pr)
			}

			return client.UpdateBranch(ctx, pr)
		},
		note: botNote("rebase via %s"),
	}
}

// mergeHow is how pr merges when its repo doesn't allow squashing, or "".
func mergeHow(pr github.PR) string {
	switch pr.MergeMethod {
	case github.MergeCommit:
		return "with a merge commit"
	case github.MergeRebase:
		return "by rebasing"
	}

	return ""
}

// closeNote says what closing a bot's PR unmerged tells the bot.
func closeNote(pr github.PR) string {
	switch pr.Bot() {
	case "Renovate":
		return "Renovate ignores this update from now on"
	case "Dependabot":
		return "Dependabot skips this version from now on"
	}

	return ""
}

// botNote notes format, filled in with the bot's name, on the PRs a bot
// manages.
func botNote(format string) func(github.PR) string {
	return func(pr github.PR) string {
		if bot := pr.Bot(); bot != "" {
			return fmt.Sprintf(format, bot)
		}

		return ""
	}
}

func (m Model) handleActionDone(msg actionDoneMsg) Model {
	m.results = msg.results
	m = m.applyOnDone(msg.results)
	m.screen = screenResults
	m.pane.GotoTop()

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

func (m Model) handleResultsKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.results == nil {
		return m, nil // still running; only ctrl+c (handled in Update) leaves
	}

	if msg.String() == keyEnter || msg.String() == keyEsc {
		m.screen = screenList
		m.selected = map[prKey]bool{}
		m = m.markClosed(m.results).withStoredDetails()
		delete(m.cache, m.query)
		m = m.refreshRows()

		return m.reload()
	}

	return m.scrollPane(msg, m.resultsBody()), nil
}

// markClosed records the PRs the action merged or closed, so they leave the
// list now and stay out of searches that still list them.
func (m Model) markClosed(results []worker.Result) Model {
	if m.action == nil || m.action.closes == nil {
		return m
	}

	for _, res := range results {
		if res.Err != nil || res.PR.ID == "" || !m.action.closes(res.PR) {
			continue
		}

		detail, ok := m.details[res.PR.ID]
		if !ok {
			detail = github.Detail{ID: res.PR.ID}
		}

		detail.Closed, detail.ClosedAt = true, time.Now()
		m.details[res.PR.ID] = detail
	}

	return m
}

func (m Model) startAction(actionKey string) (Model, tea.Cmd) {
	if len(m.targetPRs()) == 0 {
		return m, nil
	}

	if actionKey == "l" {
		m.actionKey = actionKey
		m.screen = screenActionInput
		m.actionInput.SetValue("")
		m.actionInput.Focus()

		return m, nil
	}

	// what these do to a PR depends on its details: whether it is clean, or Renovate's
	if slices.Contains([]string{"a", "c", "u", "U"}, actionKey) &&
		slices.ContainsFunc(m.targetPRs(), func(pr github.PR) bool { return !pr.Detailed }) {
		m.notice = "still loading PR details: try again in a moment"

		return m, nil
	}

	m.action = actionsForKey(m.client, actionKey, "")
	if m.action != nil && m.action.warns {
		note := m.action.note
		m.action.destructive = slices.ContainsFunc(
			m.targetPRs(),
			func(pr github.PR) bool { return note(pr) != "" },
		)
	}

	return m.enterConfirm(), nil
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
	m.confirm = m.targetPRs()
	m.screen = screenConfirm
	m.pane.GotoTop()

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

// applyOnDone runs the action's onDone on the local copy of every PR that
// succeeded, so the table reflects the change without a reload.
func (m Model) applyOnDone(results []worker.Result) Model {
	if m.action == nil || m.action.onDone == nil {
		return m
	}

	position := make(map[prKey]int, len(m.prs))
	for idx, pull := range m.prs {
		position[keyOf(pull)] = idx
	}

	for _, res := range results {
		if idx, ok := position[keyOf(res.PR)]; ok && res.Err == nil {
			m.action.onDone(&m.prs[idx])

			// so that re-applying the stored details doesn't undo the change
			if detail, known := m.details[res.PR.ID]; known {
				detail.AutoMerge = m.prs[idx].AutoMerge
				m.details[res.PR.ID] = detail
			}
		}
	}

	return m.refreshRows()
}

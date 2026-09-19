package ui

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

const progressPollInterval = 150 * time.Millisecond

type searchDoneMsg struct {
	prs []github.PR
	err error
}

type actionDoneMsg struct {
	results []worker.Result
}

type actionProgressMsg struct{}

func (m Model) search(query string) tea.Cmd {
	return func() tea.Msg {
		prs, err := m.client.SearchPRs(context.Background(), query, maxBatchSize)

		return searchDoneMsg{prs: prs, err: err}
	}
}

func enhanceCommand(pr github.PR) *exec.Cmd {
	//nolint:gosec // fixed binary, no shell; pr.URL is the https URL GitHub returned
	return exec.CommandContext(context.Background(), "gh", "enhance", pr.URL)
}

// launchEnhance hands the terminal to gh-enhance on the focused PR and
// resumes the list when it exits.
func (m Model) launchEnhance() tea.Cmd {
	pr, ok := m.focusedPR()
	if !ok {
		return nil
	}

	return tea.ExecProcess(enhanceCommand(pr), func(error) tea.Msg { return nil })
}

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
		return &pendingAction{label: "close", run: client.ClosePR}
	case "m":
		return &pendingAction{label: "merge", run: client.MergePR}
	}

	return nil
}

// needsInput reports whether the bulk action for key k needs a text prompt
// (a label name) before it can run.
func needsInput(k string) bool {
	return k == "l"
}

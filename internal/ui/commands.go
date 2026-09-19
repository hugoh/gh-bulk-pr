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

// searchDoneMsg carries one search result. id and query say which search it
// answers; light marks the fast first pass that lacks checks and merge state.
type searchDoneMsg struct {
	id    int
	query string
	light bool
	prs   []github.PR
	err   error
}

type actionDoneMsg struct {
	results []worker.Result
}

type actionProgressMsg struct{}

// searchCmds runs the full search and, when there are no rows to show yet,
// a light one alongside it so the list paints sooner. Both stop with ctx.
func (m Model) searchCmds(ctx context.Context) tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.runSearch(ctx, false)}
	if len(m.prs) == 0 {
		cmds = append(cmds, m.runSearch(ctx, true))
	}

	return tea.Batch(cmds...)
}

func (m Model) runSearch(ctx context.Context, light bool) tea.Cmd {
	client, query, searchID := m.client, m.query, m.searchID

	return func() tea.Msg {
		fetch := client.SearchPRs
		if light {
			fetch = client.SearchPRsLight
		}

		prs, err := fetch(ctx, query, maxBatchSize)

		return searchDoneMsg{id: searchID, query: query, light: light, prs: prs, err: err}
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

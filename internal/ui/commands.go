package ui

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

const progressPollInterval = 150 * time.Millisecond

// searchDoneMsg carries one page of search results. id and query say which
// search it answers; light marks the fast pass that lacks checks and merge
// state, and more marks a page after the first.
type searchDoneMsg struct {
	id      int
	query   string
	light   bool
	more    bool
	prs     []github.PR
	total   int
	cursor  string
	hasNext bool
	err     error
}

// historyLoadedMsg delivers the past queries read for the filter bar.
type historyLoadedMsg struct {
	entries []string
}

type actionDoneMsg struct {
	results []worker.Result
}

type actionProgressMsg struct{}

// searchCmds runs the full search for the first page and, when there are no
// rows to show yet, a light one alongside it so the list paints sooner. Both
// stop with ctx.
func (m Model) searchCmds(ctx context.Context) tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.runSearch(ctx, false, "", false)}
	if len(m.prs) == 0 {
		cmds = append(cmds, m.runSearch(ctx, true, "", false))
	}

	return tea.Batch(cmds...)
}

// moreCmds fetches the page after m.endCursor, light and full at once: its
// rows aren't on screen yet. Both stop with ctx.
func (m Model) moreCmds(ctx context.Context) tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.runSearch(ctx, false, m.endCursor, true),
		m.runSearch(ctx, true, m.endCursor, true),
	)
}

func (m Model) runSearch(ctx context.Context, light bool, after string, more bool) tea.Cmd {
	client, query, searchID := m.client, m.query, m.searchID

	return func() tea.Msg {
		page, err := client.SearchPage(ctx, query, after, light)

		return searchDoneMsg{
			id:      searchID,
			query:   query,
			light:   light,
			more:    more,
			prs:     page.PRs,
			total:   page.Total,
			cursor:  page.EndCursor,
			hasNext: page.HasNext,
			err:     err,
		}
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
		return &pendingAction{label: "close", destructive: true, run: client.ClosePR}
	case "m":
		return &pendingAction{label: "merge", destructive: true, run: client.MergePR}
	}

	return nil
}

// needsInput reports whether the bulk action for key k needs a text prompt
// (a label name) before it can run.
func needsInput(k string) bool {
	return k == "l"
}

// recordQuery appends query to the history file. It's a command so the file
// write stays out of Update; a failure is dropped, since history must never
// get in the way of a search.
func (m Model) recordQuery(query string) tea.Cmd {
	if m.history == nil {
		return nil
	}

	log := m.history

	return func() tea.Msg {
		_ = log.Add(query)

		return nil
	}
}

// loadHistory reads the past queries for the filter bar.
func (m Model) loadHistory() tea.Cmd {
	if m.history == nil {
		return nil
	}

	log := m.history

	return func() tea.Msg { return historyLoadedMsg{entries: log.Load()} }
}

// openFailedMsg reports that the browser couldn't be started.
type openFailedMsg struct {
	err error
}

// browse opens url the way gh does: $GH_BROWSER, gh's browser setting,
// $BROWSER, then the system default. Its output is discarded so it can't
// scribble over the TUI.
func browse(url string) error {
	if err := browser.New("", io.Discard, io.Discard).Browse(url); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	return nil
}

// openFocused opens the focused PR in the browser, off the UI thread.
func (m Model) openFocused() tea.Cmd {
	pr, ok := m.focusedPR()
	if !ok || pr.URL == "" {
		return nil
	}

	return m.openAll([]string{pr.URL})
}

// openAll opens each url in turn, off the UI thread. One failure doesn't stop
// the rest; the first is reported.
func (m Model) openAll(urls []string) tea.Cmd {
	open := m.open

	return func() tea.Msg {
		var first error

		for _, url := range urls {
			if err := open(url); err != nil && first == nil {
				first = err
			}
		}

		if first != nil {
			return openFailedMsg{err: first}
		}

		return nil
	}
}

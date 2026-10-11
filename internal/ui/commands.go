package ui

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

const progressPollInterval = 150 * time.Millisecond

// searchDoneMsg carries one page of search results. id and query say which
// search it answers, and more marks a page after the first.
type searchDoneMsg struct {
	github.Page

	id    int
	query string
	more  bool
	err   error
}

// historyLoadedMsg delivers the past queries read for the filter bar.
type historyLoadedMsg struct {
	entries []string
}

type actionDoneMsg struct {
	results []worker.Result
}

type actionProgressMsg struct{}

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

// copyFocusedURL puts the focused PR's URL on the clipboard (OSC 52, so it
// also works over SSH).
func (m Model) copyFocusedURL() tea.Cmd {
	pr, ok := m.focusedPR()
	if !ok || pr.URL == "" {
		return nil
	}

	return tea.SetClipboard(pr.URL)
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

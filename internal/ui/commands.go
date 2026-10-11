package ui

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

// historyLoadedMsg delivers the past queries read for the filter bar.
type historyLoadedMsg struct {
	entries []string
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

// Command gh-bulk-pr is a gh CLI extension for filtering, multi-selecting,
// and bulk-editing pull requests (label, reviewer, close, merge) from the
// terminal.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

var version = "dev" // set via -ldflags by goreleaser

func main() {
	query := flag.String(
		"query",
		ui.DefaultQuery,
		"GitHub search query for the PR list",
	)
	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *showVersion {
		_, _ = fmt.Fprintln(os.Stdout, "gh-bulk-pr", version)

		return
	}

	client, err := github.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-bulk-pr:", err)
		os.Exit(1)
	}

	model := ui.New(client, *query).WithHistory(history.New(history.DefaultPath()))

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gh-bulk-pr:", err)
		os.Exit(1)
	}
}

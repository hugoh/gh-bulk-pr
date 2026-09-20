//go:generate go run ./cmd/genreadme

// Command gh-bulk-pr is a gh CLI extension for filtering, multi-selecting,
// and bulk-editing pull requests (label, reviewer, close, merge) from the
// terminal.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/cli"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

var version = "dev" // set via -ldflags by goreleaser

func main() {
	opts := cli.Register(flag.CommandLine)

	flag.Parse()

	if opts.Version {
		_, _ = fmt.Fprintln(os.Stdout, "gh-bulk-pr", version)

		return
	}

	client, err := github.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-bulk-pr:", err)
		os.Exit(1)
	}

	model := ui.New(client, opts.Query).
		WithHistory(history.New(history.DefaultPath())).
		WithMouse(opts.Mouse)

	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gh-bulk-pr:", err)
		os.Exit(1)
	}
}

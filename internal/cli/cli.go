// Package cli defines the command-line flags, shared by main and the README generator.
package cli

import (
	"flag"
	"time"

	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

const defaultRefresh = 30 * time.Second

// Options holds the parsed flag values.
type Options struct {
	Query          string
	Mouse, Version bool
	Refresh        time.Duration
}

// Register defines every flag on flagSet and returns the Options they fill in.
func Register(flagSet *flag.FlagSet) *Options {
	opts := &Options{}

	flagSet.StringVar(&opts.Query, "query", ui.DefaultQuery, "GitHub search query for the PR list")
	flagSet.BoolVar(
		&opts.Mouse,
		"mouse",
		false,
		"scroll with the mouse wheel (the terminal then needs shift to select text)",
	)
	flagSet.DurationVar(
		&opts.Refresh,
		"refresh",
		defaultRefresh,
		"how often to re-check PRs whose checks are pending, e.g. 30s (0 turns it off)",
	)
	flagSet.BoolVar(&opts.Version, "version", false, "print version and exit")

	return opts
}

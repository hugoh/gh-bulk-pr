// Command genreadme renders README.md from README.md.tpl using values read
// from the code, so the documented flags, keys and limits cannot drift.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/hugoh/gh-bulk-pr/internal/cli"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

func flagsTable() string {
	flagSet := flag.NewFlagSet("gh-bulk-pr", flag.ContinueOnError)
	cli.Register(flagSet)

	var out strings.Builder

	out.WriteString("| Flag | Default | Description |\n| --- | --- | --- |\n")
	flagSet.VisitAll(func(f *flag.Flag) {
		fmt.Fprintf(&out, "| `--%s` | `%s` | %s |\n", f.Name, f.DefValue, f.Usage)
	})

	return out.String()
}

func keysTable() string {
	var out strings.Builder

	out.WriteString("| Key | Action |\n| --- | --- |\n")

	for _, k := range ui.HelpKeys() {
		h := k.Help()
		fmt.Fprintf(&out, "| `%s` | %s |\n", h.Key, h.Desc)
	}

	return out.String()
}

func run() error {
	tpl, err := template.ParseFiles("README.md.tpl")
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	out, err := os.Create("README.md")
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}

	data := map[string]any{
		"FlagsTable":          flagsTable(),
		"KeysTable":           keysTable(),
		"BaseQuery":           ui.BaseQuery,
		"DefaultQuery":        ui.DefaultQuery,
		"Tabs":                ui.TabNames(),
		"PageSize":            github.SearchPageSize,
		"OpenAllConfirmAbove": ui.OpenAllConfirmAbove,
		"OpenAllMax":          ui.OpenAllMax,
		"MinWidth":            ui.MinWidth,
		"MinHeight":           ui.MinHeight,
	}

	if err := tpl.Execute(out, data); err != nil {
		_ = out.Close()

		return fmt.Errorf("executing template: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing output: %w", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

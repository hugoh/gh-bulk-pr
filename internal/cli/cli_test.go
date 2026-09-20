package cli_test

import (
	"flag"
	"testing"

	"github.com/hugoh/gh-bulk-pr/internal/cli"
	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

func TestRegisterDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := cli.Register(fs)

	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}

	if opts.Query != ui.DefaultQuery || opts.Mouse || opts.Version {
		t.Errorf("unexpected defaults: %+v", opts)
	}
}

func TestRegisterParses(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := cli.Register(fs)

	if err := fs.Parse([]string{"-query", "x", "-mouse", "-version"}); err != nil {
		t.Fatal(err)
	}

	if opts.Query != "x" || !opts.Mouse || !opts.Version {
		t.Errorf("flags not parsed: %+v", opts)
	}
}

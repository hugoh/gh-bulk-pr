package cli_test

import (
	"flag"
	"testing"
	"time"

	"github.com/hugoh/gh-bulk-pr/internal/cli"
	"github.com/hugoh/gh-bulk-pr/internal/ui"
)

func TestRegisterDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := cli.Register(fs)

	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}

	if opts.Query != ui.DefaultQuery || opts.Mouse || opts.Version ||
		opts.Refresh != 30*time.Second {
		t.Errorf("unexpected defaults: %+v", opts)
	}
}

func TestRegisterParses(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := cli.Register(fs)

	if err := fs.Parse(
		[]string{"-query", "x", "-mouse", "-version", "-refresh", "5s"},
	); err != nil {
		t.Fatal(err)
	}

	if opts.Query != "x" || !opts.Mouse || !opts.Version || opts.Refresh != 5*time.Second {
		t.Errorf("flags not parsed: %+v", opts)
	}
}

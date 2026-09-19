// Package history keeps an append-only log of searches under the XDG state
// directory.
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirPerm  = 0o755
	filePerm = 0o600
)

// Log appends queries to a file, one per line. A nil *Log discards everything.
type Log struct {
	path string
	last string
}

// New returns a Log writing to path.
func New(path string) *Log {
	return &Log{path: path}
}

// DefaultPath is $XDG_STATE_HOME/gh-bulk-pr/history, defaulting to
// ~/.local/state when XDG_STATE_HOME is unset.
func DefaultPath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}

	return filepath.Join(state, "gh-bulk-pr", "history")
}

// Add appends query unless it repeats the previous entry.
// ponytail: unbounded append-only file, truncate on load if it ever gets big.
func (l *Log) Add(query string) error {
	if l == nil || query == l.last {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(l.path), dirPerm); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}

	_, writeErr := file.WriteString(query + "\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	l.last = query

	return nil
}

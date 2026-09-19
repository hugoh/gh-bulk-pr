// Package history keeps an append-only log of searches under the XDG state
// directory.
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const (
	dirPerm  = 0o755
	filePerm = 0o600
)

// Log appends queries to a file, one per line. A nil *Log discards everything.
// It is safe for concurrent use.
type Log struct {
	path string

	mu   sync.Mutex
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
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if query == l.last {
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

// Load returns the logged queries, oldest first, each one once at its most
// recent use. A missing or unreadable file is an empty history.
func (l *Log) Load() []string {
	if l == nil {
		return nil
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	seen := map[string]bool{}

	var newestFirst []string

	for _, query := range slices.Backward(lines) {
		if query != "" && !seen[query] {
			seen[query] = true
			newestFirst = append(newestFirst, query)
		}
	}

	slices.Reverse(newestFirst)

	return newestFirst
}

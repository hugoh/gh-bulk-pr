package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPath(t *testing.T) {
	t.Run("uses XDG_STATE_HOME", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/xdg/state")
		assert.Equal(t, "/xdg/state/gh-bulk-pr/history", DefaultPath())
	})

	t.Run("falls back to ~/.local/state", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/home/u")
		assert.Equal(t, "/home/u/.local/state/gh-bulk-pr/history", DefaultPath())
	})
}

func TestLog_Add(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sub", "history")
	log := New(path)

	require.NoError(t, log.Add("is:open a"))
	require.NoError(t, log.Add("is:open a"), "consecutive duplicate is skipped")
	require.NoError(t, log.Add("is:open b"))
	require.NoError(t, log.Add("is:open a"))

	got, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	require.NoError(t, err)
	assert.Equal(t, "is:open a\nis:open b\nis:open a\n", string(got))
}

func TestLog_NilIsNoop(t *testing.T) {
	t.Parallel()

	var log *Log

	assert.NoError(t, log.Add("q"))
}

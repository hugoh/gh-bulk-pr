package ui

import (
	"testing"

	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		key  string
		want bool
	}{
		textLabel:   {"l", true},
		actionClose: {"c", false},
		actionMerge: {"m", false},
		textAuto:    {"a", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, needsInput(tt.key))
		})
	}
}

func TestActionsForKey(t *testing.T) {
	t.Parallel()

	client := &github.Client{}

	tests := map[string]struct {
		key       string
		input     string
		wantLabel string
	}{
		textLabel:   {"l", "bug", `add label "bug"`},
		actionClose: {"c", "", actionClose},
		actionMerge: {"m", "", actionMerge},
		textAuto:    {"a", "", "toggle auto-merge"},
		"unknown":   {"z", "", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			action := actionsForKey(client, tt.key, tt.input)

			if tt.wantLabel == "" {
				assert.Nil(t, action)

				return
			}

			require.NotNil(t, action)
			assert.Equal(t, tt.wantLabel, action.label)
			assert.NotNil(t, action.run)
		})
	}
}

func TestPollActionProgress(t *testing.T) {
	t.Parallel()

	cmd := pollActionProgress()
	require.NotNil(t, cmd)

	msg := cmd()
	_, ok := msg.(actionProgressMsg)
	assert.True(t, ok)
}

func TestActionsForKey_Destructive(t *testing.T) {
	t.Parallel()

	client := &github.Client{}

	assert.False(t, actionsForKey(client, "l", "bug").destructive, "labelling is easy to undo")
	assert.True(t, actionsForKey(client, "c", "").destructive)
	assert.True(t, actionsForKey(client, "m", "").destructive)
	assert.False(
		t,
		actionsForKey(client, "a", "").destructive,
		"toggling auto-merge is easy to undo",
	)
}

func TestActionsForKey_AutoMergeLeavesAMergedPRAlone(t *testing.T) {
	t.Parallel()

	action := actionsForKey(&github.Client{}, "a", "")
	pull := github.PR{MergeState: mergeClean}

	action.onDone(&pull)
	assert.False(t, pull.AutoMerge, "a clean PR was merged, not given auto-merge")
}

func TestActionsForKey_AutoMergeFlipsLocalState(t *testing.T) {
	t.Parallel()

	action := actionsForKey(&github.Client{}, "a", "")
	require.NotNil(t, action.onDone)

	pr := github.PR{AutoMerge: false}
	action.onDone(&pr)
	assert.True(t, pr.AutoMerge)

	action.onDone(&pr)
	assert.False(t, pr.AutoMerge)
}

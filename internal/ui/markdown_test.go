package ui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	renovateBody = "<!-- hidden note -->\n\n" +
		"| Package | Change |\n| --- | --- |\n| foo | `1.0` -> `1.1` |\n\n" +
		"See [the notes](https://example.com/notes).\n"
	testWidth = 60
)

func TestMarkdownCache_RendersMarkdownAndDropsComments(t *testing.T) {
	t.Parallel()

	got := ansi.Strip(
		newMarkdownCache().body(github.PR{ID: "a", Body: renovateBody}, testWidth, 50),
	)

	assert.Contains(t, got, "Package")
	assert.Contains(t, got, "the notes")
	assert.NotContains(t, got, "| --- |")
	assert.NotContains(t, got, "](https://")
	assert.NotContains(t, got, "hidden note")
}

func TestMarkdownCache_EmptyBody(t *testing.T) {
	t.Parallel()

	assert.Empty(t, newMarkdownCache().body(github.PR{ID: "a"}, testWidth, 50))
}

func TestMarkdownCache_LinesFitTheWidth(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 80)
	got := newMarkdownCache().body(github.PR{ID: "a", Body: long}, testWidth, 50)

	for line := range strings.SplitSeq(got, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), testWidth)
	}
}

func TestMarkdownCache_CutsByLinesWithAnEllipsis(t *testing.T) {
	t.Parallel()

	items := strings.Repeat("- item\n", 30)
	got := newMarkdownCache().body(github.PR{ID: "a", Body: items}, testWidth, 5)

	assert.Equal(t, 5, lipgloss.Height(got))
	assert.True(t, strings.HasSuffix(ansi.Strip(got), "…"))
}

func TestMarkdownCache_RendersOncePerPRAndWidth(t *testing.T) {
	t.Parallel()

	cache := newMarkdownCache()
	pr := github.PR{ID: "a", Body: "# hi"}

	cache.body(pr, testWidth, 10)
	cache.body(pr, testWidth, 10)
	assert.Equal(t, 1, cache.renders)

	cache.body(pr, testWidth+10, 10)
	assert.Equal(t, 2, cache.renders, "a resize renders again")

	cache.body(github.PR{ID: "a", Body: "# changed"}, testWidth, 10)
	assert.Equal(t, 3, cache.renders, "a new body renders again")
}

// Not parallel: it swaps the global compat.HasDarkBackground.
func TestMarkdownCache_FollowsTheTerminalBackground(t *testing.T) {
	prev := compat.HasDarkBackground

	t.Cleanup(func() { compat.HasDarkBackground = prev })

	pr := github.PR{ID: "a", Body: "# hi"}
	cache := newMarkdownCache()

	compat.HasDarkBackground = true
	dark := cache.body(pr, testWidth, 10)

	compat.HasDarkBackground = false
	light := cache.body(pr, testWidth, 10)

	assert.NotEqual(t, dark, light)
}

func TestPreviewPanel_FitsTheTerminalHeight(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.previewOpen = true
	m = m.syncTableHeight()
	m.prs = slices.Clone(m.prs)
	m.prs[0].Body = strings.Repeat("- item\n", 100)
	m.prs[0].Detailed = true
	m = m.refreshRows()

	require.NotEmpty(t, m.prs)
	assert.LessOrEqual(t, lipgloss.Height(m.viewString()), 30)
	assert.Contains(t, ansi.Strip(m.viewString()), "item")
}

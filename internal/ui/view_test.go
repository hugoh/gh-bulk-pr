package ui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const checksPassingWord = "passing"

func TestPrNumber(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "#42", prNumber(42))
	assert.Equal(t, "#0", prNumber(0))
}

func TestChecksSummary(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   github.PR
		want string
	}{
		"fail": {github.PR{Checks: github.ChecksFail}, "✗"},
		"pass": {github.PR{Checks: github.ChecksPass}, "✓"},
		"none": {github.PR{}, "-"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, checksSummary(tt.pr))
		})
	}
}

func TestPreviewChecks(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   github.PR
		want string
	}{
		"failing": {github.PR{Checks: github.ChecksFail}, "failing"},
		"passing": {github.PR{Checks: github.ChecksPass}, checksPassingWord},
		"none":    {github.PR{}, "no checks"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, previewChecks(tt.pr), tt.want)
		})
	}
}

func TestFooterText(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	assert.Contains(t, m.footerText(), "filter")
	assert.Contains(t, m.footerText(), "T checks")

	m.selected[keyOf(m.prs[0])] = true
	assert.Contains(t, m.footerText(), "1 selected")
}

func TestPreviewText(t *testing.T) {
	t.Parallel()

	pr := github.PR{
		Number:     5,
		Title:      prTitleFix,
		Repo:       testRepoA,
		Author:     testAuthor,
		Labels:     []string{"bug", "priority"},
		Reviewers:  []string{"alice"},
		MergeState: mergeBehind,
		Body:       "some description",
	}

	got := previewText(pr)
	assert.Contains(t, got, "merge: behind")
	assert.Contains(t, got, prTitleFix)
	assert.Contains(t, got, testRepoA)
	assert.Contains(t, got, "#5")
	assert.Contains(t, got, "bug, priority")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "some description")
}

func TestPreviewText_TruncatesLongBody(t *testing.T) {
	t.Parallel()

	longBody := make([]byte, previewBodyLimit+100)
	for i := range longBody {
		longBody[i] = 'a'
	}

	got := previewText(github.PR{Body: string(longBody)})
	assert.Contains(t, got, "…")
	assert.NotContains(t, got, string(longBody))
}

func TestViewList_Loading(t *testing.T) {
	t.Parallel()

	m := New(nil, "is:open is:pr")
	assert.Contains(t, m.View(), "loading")
}

func TestViewList_Error(t *testing.T) {
	t.Parallel()

	m := New(nil, "q")
	m = m.handleSearchDone(searchDoneMsg{err: errors.New("network down")})
	assert.Contains(t, m.View(), "network down")
}

func TestViewList_ShowsFooterAndTable(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	view := m.View()
	assert.Contains(t, view, prTitleFix)
	assert.Contains(t, view, "filter")
}

func TestViewList_PreviewOpen(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.previewOpen = true

	view := m.View()
	assert.Contains(t, view, prTitleFix)
	assert.Contains(t, view, "─") // separator line above the preview
}

func TestView_FilterScreen(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenFilter
	assert.Contains(t, m.View(), "Search:")
}

func TestView_ActionInputScreen(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenActionInput
	assert.Contains(t, m.View(), "Label:")
}

func TestViewConfirm(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenConfirm
	m.action = &pendingAction{label: actionClose}
	m.confirm = testPRs()

	got := m.View()
	assert.Contains(t, got, actionClose)
	assert.Contains(t, got, prTitleFix)
	assert.Contains(t, got, "Add feature")
}

func TestViewActionProgress(t *testing.T) {
	t.Parallel()

	var done atomic.Int32
	done.Store(1)

	m := loadedModel()
	m.screen = screenResults
	m.action = &pendingAction{label: actionClose}
	m.actionDone = &done
	m.actionTotal = 2

	got := m.View()
	assert.Contains(t, got, actionClose)
	assert.Contains(t, got, "1/2")
}

func TestViewResults(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenResults
	m.results = []worker.Result{
		{PR: github.PR{Number: 1, Title: prTitleFix}},
		{PR: github.PR{Number: 2, Title: "Add feature"}, Err: errors.New("403")},
	}

	got := m.View()
	assert.Contains(t, got, prTitleFix)
	assert.Contains(t, got, "Add feature")
	assert.Contains(t, got, "403")
}

// Not parallel: it swaps the global lipgloss color profile.
func TestCheckGlyphsAreColored(t *testing.T) {
	prev := lipgloss.ColorProfile()

	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := loadedModel()
	m.screen = screenResults
	m.results = []worker.Result{
		{PR: github.PR{Number: 1}},
		{PR: github.PR{Number: 2}, Err: errors.New("403")},
	}
	got := m.View()
	assert.Contains(t, got, okStyle().Render("✓"))
	assert.Contains(t, got, errStyle().Render("✗ 403"))
}

func TestColorChecks_KeepsColorAndHighlightOnSelectedRow(t *testing.T) {
	prev := lipgloss.ColorProfile()

	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	sel, _, _ := strings.Cut(table.DefaultStyles().Selected.Render("|"), "|")
	require.NotEmpty(t, sel)

	got := colorChecks("plain ✓\n" + sel + "row ✗ tail\x1b[0m")

	assert.Contains(t, got, "plain "+okStyle().Render("✓")+"\n")
	assert.Contains(t, got, errStyle().Render("✗")+sel+" tail")
	assert.NotContains(t, got, okStyle().Render("✓")+sel)
}

func TestMergeLabel(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		mergeClean:  "clean",
		mergeBehind: "behind",
		mergeDirty:  labelConflict,
		"BLOCKED":   "blocked",
		"UNSTABLE":  "unstable",
		"DRAFT":     "draft",
		"HAS_HOOKS": "hooks",
		"UNKNOWN":   "unknown",
		"":          "-",
		"NEW_STATE": "-",
	}

	for state, want := range tests {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, mergeLabel(state))
		})
	}
}

func TestMergeSummary(t *testing.T) {
	t.Parallel()

	prs := []github.PR{
		{MergeState: mergeClean},
		{MergeState: mergeBehind},
		{MergeState: mergeDirty},
		{MergeState: mergeBehind},
		{MergeState: ""},
	}

	assert.Equal(t, "2 behind · 1 conflict", mergeSummary(prs))
	assert.Empty(t, mergeSummary([]github.PR{{MergeState: mergeClean}}))
	assert.Empty(t, mergeSummary(nil))
}

func TestViewList_ShowsMergeSummary(t *testing.T) {
	t.Parallel()

	got := loadedModel().View()
	assert.Contains(t, got, "1 behind")
}

func TestMergeStyle(t *testing.T) {
	t.Parallel()

	assert.Equal(t, okStyle().GetForeground(), mergeStyle("clean").GetForeground())
	assert.Equal(t, previewLabelStyle().GetForeground(), mergeStyle("behind").GetForeground())
	assert.Equal(t, errStyle().GetForeground(), mergeStyle("conflict").GetForeground())
	assert.Equal(t, errStyle().GetForeground(), mergeStyle("blocked").GetForeground())
	assert.Equal(t, previewMetaStyle().GetForeground(), mergeStyle("draft").GetForeground())
	assert.Equal(t, previewMetaStyle().GetForeground(), mergeStyle("-").GetForeground())
}

// Not parallel: it swaps the global lipgloss color profile.
func TestColorMerge_ColorsOnlyTheMergeColumn(t *testing.T) {
	prev := lipgloss.ColorProfile()

	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.handleSearchDone(searchDoneMsg{prs: []github.PR{
		{Number: 1, Title: "fix", Repo: testRepoA, Author: testAuthor, MergeState: mergeBehind},
		{
			Number:     2,
			Title:      "clean up behind",
			Repo:       testRepoB,
			Author:     testAuthor,
			MergeState: mergeClean,
		},
		{Number: 3, Title: "other", Repo: "hugoh/c", Author: testAuthor, MergeState: mergeDirty},
	}})

	plain := m.table.View()
	got := colorMerge(plain, m.table.Columns())
	assert.Equal(t, ansi.Strip(plain), ansi.Strip(got), "layout must not shift")

	sel, _, _ := strings.Cut(table.DefaultStyles().Selected.Render("|"), "|")
	lines := strings.Split(got, "\n")

	assert.Contains(
		t,
		lines[1],
		mergeStyle("behind").Render("behind")+sel,
		"cursor row keeps highlight",
	)
	assert.Contains(t, lines[2], mergeStyle("clean").Render("clean"))
	assert.Contains(t, lines[2], "clean up behind", "title untouched")
	assert.NotContains(t, lines[2], mergeStyle("behind").Render("behind"))
	assert.Contains(t, lines[3], mergeStyle("conflict").Render("conflict"))
	assert.Equal(
		t,
		strings.Split(plain, "\n")[0],
		lines[0],
		"header untouched",
	)
}

func TestPreviewMerge(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "merge: conflict", ansi.Strip(previewMerge(github.PR{MergeState: mergeDirty})))
	assert.Equal(t, "merge: -", ansi.Strip(previewMerge(github.PR{})))
}

func TestViewList_RefreshingKeepsRows(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().reload()
	view := m.View()

	assert.Contains(t, view, prTitleFix, "rows stay visible during a refresh")
	assert.Contains(t, view, "refreshing")
}

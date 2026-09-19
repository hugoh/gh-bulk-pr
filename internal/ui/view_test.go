package ui

import (
	"errors"
	"fmt"
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
	assert.Contains(t, m.footerText(), textFilter)
	assert.Contains(t, m.footerText(), "? help", "keys that don't fit live behind ?")

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
	assert.Contains(t, view, textFilter)
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
	assert.Contains(t, m.View(), "Label for")
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
		{
			Number:     1,
			Title:      "fix",
			Repo:       testRepoA,
			Author:     testAuthor,
			MergeState: mergeBehind,
			Detailed:   true,
		},
		{
			Number:     2,
			Title:      "clean up behind",
			Repo:       testRepoB,
			Author:     testAuthor,
			MergeState: mergeClean,
			Detailed:   true,
		},
		{
			Number:     3,
			Title:      "other",
			Repo:       "hugoh/c",
			Author:     testAuthor,
			MergeState: mergeDirty,
			Detailed:   true,
		},
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

func TestViewList_ShowsLoadedOfTotal(t *testing.T) {
	t.Parallel()

	assert.Contains(t, pagedModel().View(), "1 of 312 · 50 loaded")
	assert.NotContains(
		t,
		loadedModel().View(),
		"loaded",
		"a complete list has nothing left to load",
	)
}

func TestFooter_SelectedOfTotal(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.selected[keyOf(m.prs[0])] = true

	assert.Contains(t, m.footerText(), "1 selected of 312")
}

func TestViewList_LoadingMore(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.loadingMore = true

	assert.Contains(t, m.View(), "loading more")
}

func TestViewList_StatusOnSecondLine(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.prs[0].MergeState = mergeBehind
	m.loadingMore = true

	lines := strings.Split(m.View(), "\n")

	assert.Contains(t, lines[0], "gh-bulk-pr")
	assert.NotContains(t, lines[0], "312")
	assert.NotContains(t, lines[0], "behind")

	for _, want := range []string{"1 behind", "1 of 312 · 50 loaded", "loading more"} {
		assert.Contains(t, lines[1], want)
	}
}

func TestViewList_EmptyResultKeepsSpacerLine(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.handleSearchDone(searchDoneMsg{prs: nil})

	lines := strings.Split(m.View(), "\n")

	assert.Empty(t, strings.TrimSpace(lines[1]))
	assert.Contains(t, lines[2], "Repo", "table header follows the spacer")
}

func TestViewList_StatusIsRightAligned(t *testing.T) {
	t.Parallel()

	line := strings.Split(pagedModel().View(), "\n")[1]

	assert.Equal(t, 100, lipgloss.Width(line), "padded to the full width")
	assert.True(t, strings.HasSuffix(strings.TrimRight(line, " "), "50 loaded"))
	assert.True(t, strings.HasPrefix(line, " "), "text sits at the right, not the left")
}

func TestViewList_StatusShowsCursorPosition(t *testing.T) {
	t.Parallel()

	m := pagedModel()
	m.table.SetCursor(36)

	assert.Contains(t, m.View(), "37 of 312 · 50 loaded")

	small := loadedModel()
	small.table.SetCursor(1)
	assert.Contains(t, small.View(), "2 of 2", "complete lists show position out of what's loaded")
}

func TestViewList_NoPositionWithoutRows(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m = m.handleSearchDone(searchDoneMsg{prs: nil})

	assert.NotContains(t, m.statusLine(), " of ")
}

func TestViewList_StatusShowsSelectionCount(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	assert.NotContains(t, m.statusLine(), "selected")

	m.selected[keyOf(m.prs[0])] = true
	m.selected[keyOf(m.prs[1])] = true

	assert.Contains(t, m.statusLine(), "2 selected")
}

func TestViewActionInput_ShowsCount(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("ctrl+a")
	m, _ = m.handleListKeyByString("l")

	assert.Contains(t, m.View(), "Label for 2 PR(s):")
}

func TestViewConfirm_RepeatsCountAtThePrompt(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("ctrl+a")
	m, _ = m.handleListKeyByString("c")

	view := m.View()
	lastLine := view[strings.LastIndex(view, "\n")+1:]

	assert.Contains(t, lastLine, "confirm 2 PR(s)")
}

func TestViewConfirm_PromptMatchesTheKeysThatWork(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenConfirm
	m.confirm = testPRs()

	m.action = &pendingAction{label: "add label", destructive: false}
	assert.Contains(t, m.View(), "y/enter to confirm 2 PR(s)")

	m.action = &pendingAction{label: actionMerge, destructive: true}
	view := m.View()
	assert.Contains(t, view, "y to confirm 2 PR(s)")
	assert.NotContains(t, view, "y/enter")
}

func TestFit(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in    string
		width int
		want  string
	}{
		"truncates with an ellipsis": {in: "hello world", width: 5, want: "hell…"},
		"leaves short text alone":    {in: textShort, width: 10, want: textShort},
		"zero width is a no-op":      {in: "anything at all", width: 0, want: "anything at all"},
		"each line separately":       {in: "abcdefgh\nab", width: 4, want: "abc…\nab"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, fit(tt.in, tt.width))
		})
	}

	styled := errStyle().Render(strings.Repeat("x", 50))
	assert.LessOrEqual(
		t,
		lipgloss.Width(fit(styled, 10)),
		10,
		"escape sequences don't count towards width",
	)
}

func maxLineWidth(view string) int {
	widest := 0

	for line := range strings.SplitSeq(view, "\n") {
		widest = max(widest, lipgloss.Width(line))
	}

	return widest
}

func TestView_NeverWiderThanTheTerminal(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("q", 500)

	tests := map[string]func() Model{
		"long query": func() Model {
			m := loadedModel()
			m.query = long

			return m
		},
		"busy status line": func() Model {
			m := pagedModel()
			m.selected[keyOf(m.prs[0])] = true
			m.loading, m.loadingMore = true, true
			m.moreErr = errors.New(long)

			return m
		},
		"footer with a selection": func() Model {
			m := loadedModel()
			m.selected[keyOf(m.prs[0])] = true

			return m
		},
		"footer without a selection": loadedModel,
		"filter prompt": func() Model {
			m := loadedModel()
			m.screen = screenFilter
			m.filterInput.SetValue(long)

			return m
		},
		"confirm with a long title": func() Model {
			m := loadedModel()
			m.screen = screenConfirm
			m.action = &pendingAction{label: actionClose}
			m.confirm = []github.PR{{Number: 1, Repo: testRepoA, Title: long}}

			return m
		},
	}

	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.LessOrEqual(t, maxLineWidth(build().View()), 100)
		})
	}
}

func TestView_TerminalTooSmall(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		width, height int
		want          bool
	}{
		"big enough":       {width: minWidth, height: minHeight, want: false},
		"one column short": {width: minWidth - 1, height: 30, want: true},
		"one row short":    {width: 100, height: minHeight - 1, want: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := loadedModel().handleResize(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})

			assert.Equal(t, tt.want, strings.Contains(m.View(), "terminal too small"))

			if tt.want {
				assert.Contains(
					t,
					m.View(),
					fmt.Sprintf("need %d×%d, have %d×%d", minWidth, minHeight, tt.width, tt.height),
				)
				assert.LessOrEqual(t, maxLineWidth(m.View()), tt.width)
			}
		})
	}
}

func TestView_UnknownSizeIsNotTooSmall(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, New(nil, "q").View(), "terminal too small")
}

func TestView_PromptsWorkInSmallTerminals(t *testing.T) {
	t.Parallel()

	m := loadedModel().handleResize(tea.WindowSizeMsg{Width: 40, Height: 8})
	m.screen = screenFilter

	assert.NotContains(t, m.View(), "terminal too small", "only the table needs the minimum size")
}

func lineCount(view string) int { return strings.Count(view, "\n") + 1 }

// bigConfirmModel is a 100x30 terminal about to close 200 PRs.
func bigConfirmModel() Model {
	m := loadedModel()
	m.screen = screenConfirm
	m.action = &pendingAction{label: actionClose, destructive: true}
	m.confirm = manyPRs(1, 200, true)

	return m
}

func TestViewConfirm_LongListScrolls(t *testing.T) {
	t.Parallel()

	m := bigConfirmModel()
	view := m.View()

	assert.LessOrEqual(t, lineCount(view), 30)
	assert.Contains(t, view, "About to close on 200 PR(s)", "the heading stays pinned")
	assert.Contains(t, view, "y to confirm 200 PR(s)", "the prompt stays pinned")
	assert.Contains(t, view, "scroll", "the prompt says the list scrolls")
	assert.Contains(t, view, "hugoh/a #1  PR")
	assert.NotContains(t, view, "#200")

	m, _ = m.handleConfirmKey(keyMsgFromString("G"))
	view = m.View()

	assert.Contains(t, view, "#200")
	assert.NotContains(t, view, "hugoh/a #1  PR")
	assert.Contains(t, view, "About to close on 200 PR(s)")
	assert.LessOrEqual(t, lineCount(view), 30)
}

func TestViewConfirm_ShortListHasNoScrollHint(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenConfirm
	m.action = &pendingAction{label: actionClose}
	m.confirm = testPRs()

	assert.NotContains(t, m.View(), "scroll")
}

func TestConfirmScrollKeys(t *testing.T) {
	t.Parallel()

	m := bigConfirmModel()

	m, _ = m.handleConfirmKey(keyMsgFromString("j"))
	assert.Equal(t, 1, m.pane.YOffset)

	m, _ = m.handleConfirmKey(keyMsgFromString("G"))
	assert.Positive(t, m.pane.YOffset)

	m, _ = m.handleConfirmKey(keyMsgFromString("g"))
	assert.Zero(t, m.pane.YOffset)

	same, _ := m.handleConfirmKey(keyMsgFromString("j"))
	assert.Equal(t, screenConfirm, same.screen, "scrolling doesn't confirm or cancel")
}

func TestEnteringConfirmStartsAtTheTop(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.pane.SetContent(strings.Repeat("x\n", 100))
	m.pane.YOffset = 5

	m, _ = m.handleListKeyByString("ctrl+a")
	m, _ = m.handleListKeyByString("c")

	assert.Equal(t, screenConfirm, m.screen)
	assert.Zero(t, m.pane.YOffset)
}

func TestResize_SizesThePane(t *testing.T) {
	t.Parallel()

	m := loadedModel().handleResize(tea.WindowSizeMsg{Width: 120, Height: 40})

	assert.Equal(t, 120, m.pane.Width)
	assert.Equal(t, 40-paneChrome, m.pane.Height)
}

func bigResultsModel() Model {
	prs := manyPRs(1, 200, true)
	res := make([]worker.Result, len(prs))

	for i, pr := range prs {
		res[i] = worker.Result{PR: pr}
	}

	res[198].Err = errors.New("boom 198")
	res[199].Err = errors.New("boom 199")

	m := loadedModel()
	m.screen = screenResults
	m.action = &pendingAction{label: actionClose}
	m.results = res

	return m
}

func TestViewResults_FailuresFirstAndSummarised(t *testing.T) {
	t.Parallel()

	m := bigResultsModel()
	view := m.View()

	assert.LessOrEqual(t, lineCount(view), 30)
	assert.Contains(t, view, "Results: 198 ok · 2 failed")
	assert.Contains(t, view, "boom 198", "failures are visible without scrolling")
	assert.Contains(t, view, "boom 199")
	assert.Less(
		t,
		strings.Index(view, "boom 198"),
		strings.Index(view, "#1  PR"),
		"failures come before successes",
	)
	assert.Contains(t, view, "enter/esc to continue")
	assert.Contains(t, view, "scroll")
}

func TestViewResults_AllOK(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.screen = screenResults
	m.results = []worker.Result{{PR: github.PR{Number: 1}}}

	assert.Contains(t, m.View(), "Results: 1 ok")
	assert.NotContains(t, m.View(), "failed")
}

func TestResultsScrollKeys(t *testing.T) {
	t.Parallel()

	m := bigResultsModel()

	m, _ = m.handleResultsKey(keyMsgFromString("j"))
	assert.Equal(t, 1, m.pane.YOffset)
	assert.Equal(t, screenResults, m.screen, "scrolling doesn't leave the results")

	m, _ = m.handleResultsKey(keyMsgFromString("G"))
	assert.Positive(t, m.pane.YOffset)
}

func TestActionDoneStartsResultsAtTheTop(t *testing.T) {
	t.Parallel()

	m := bigResultsModel()
	m.results = nil
	m.pane.YOffset = 7

	updated, _ := m.Update(actionDoneMsg{results: bigResultsModel().results})

	mm, ok := updated.(Model)
	require.True(t, ok)
	assert.Zero(t, mm.pane.YOffset)
}

func lastLine(view string) string { return view[strings.LastIndex(view, "\n")+1:] }

func TestFilterPrompt_KeepsTheListVisible(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().handleListKeyByString("/")
	view := m.View()
	lines := strings.Split(view, "\n")

	assert.Contains(t, view, prTitleFix, "the list stays on screen while editing the query")
	assert.Contains(t, lines[0], "gh-bulk-pr")
	assert.Contains(t, lines[1], "esc cancel", "the key hints replace the status line")
	assert.Contains(t, lastLine(view), "Search: ")
	assert.Contains(t, lastLine(view), m.query, "the prompt starts with the current query")
	assert.LessOrEqual(t, len(lines), 30)
}

func TestLabelPrompt_KeepsTheListVisible(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("ctrl+a")
	m, _ = m.handleListKeyByString("l")
	view := m.View()

	assert.Contains(t, view, prTitleFix)
	assert.Contains(t, lastLine(view), "Label for 2 PR(s): ")
	assert.Contains(t, strings.Split(view, "\n")[1], "esc cancel")
}

func TestPrompt_ShownWhileTheFirstSearchIsStillLoading(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").handleResize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.startFilter()

	view := m.View()

	assert.Contains(t, view, "loading")
	assert.Contains(t, lastLine(view), "Search: ")
}

func TestPrompt_LongInputScrollsInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	m, _ := loadedModel().handleListKeyByString("/")
	m.filterInput.SetValue(strings.Repeat("q", 500))

	assert.LessOrEqual(t, lipgloss.Width(lastLine(m.View())), 100)
	assert.Positive(
		t,
		m.filterInput.Width,
		"a width lets the input scroll to keep the cursor in view",
	)
}

func TestPrompt_FallsBackToTheBarePromptWhenTheListCannotFit(t *testing.T) {
	t.Parallel()

	m := loadedModel().handleResize(tea.WindowSizeMsg{Width: 50, Height: 8})
	m, _ = m.startFilter()

	view := m.View()

	assert.NotContains(t, view, "terminal too small")
	assert.Contains(t, view, "Search: ")
	assert.Contains(t, view, "esc")
}

// Not parallel: it swaps the global lipgloss color profile and background.
func TestStylesAdaptToTheBackground(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	prevDark := lipgloss.HasDarkBackground()

	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		lipgloss.SetHasDarkBackground(prevDark)
	})

	tests := map[string]struct {
		render      func(string) string
		light, dark string
	}{
		"ok": {func(s string) string { return okStyle().Render(s) }, "38;5;28m", "38;5;42m"},
		"error": {
			func(s string) string { return errStyle().Render(s) },
			"38;5;160m",
			"38;5;196m",
		},
		"help": {
			func(s string) string { return helpStyle().Render(s) },
			"38;5;243m",
			"38;5;241m",
		},
		"label": {
			func(s string) string { return previewLabelStyle().Render(s) },
			"38;5;166m",
			"38;5;214m",
		},
		"reviewer": {
			func(s string) string { return previewReviewerStyle().Render(s) },
			"38;5;27m",
			"38;5;39m",
		},
	}

	for name, tt := range tests {
		lipgloss.SetHasDarkBackground(false)
		assert.Contains(t, tt.render("x"), tt.light, name+" on a light background")

		lipgloss.SetHasDarkBackground(true)
		assert.Contains(t, tt.render("x"), tt.dark, name+" on a dark background")
	}
}

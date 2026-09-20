package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

const (
	previewBodyLimit = 500
	mergeClean       = "CLEAN"
	mergeBehind      = "BEHIND"
	mergeDirty       = "DIRTY"
	labelBehind      = "behind"
	labelConflict    = "conflict"
	labelBlocked     = "blocked"
)

// adaptive is a colour that reads on both light and dark terminal backgrounds.
func adaptive(light, dark string) compat.AdaptiveColor {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}

func fg(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(adaptive(light, dark))
}

func helpStyle() lipgloss.Style            { return fg("243", "241") }
func errStyle() lipgloss.Style             { return fg("160", "196") }
func okStyle() lipgloss.Style              { return fg("28", "42") }
func headerStyle() lipgloss.Style          { return lipgloss.NewStyle().Bold(true) }
func separatorStyle() lipgloss.Style       { return fg("250", "240") }
func footerStyle() lipgloss.Style          { return helpStyle().Padding(0, 1) }
func previewTitleStyle() lipgloss.Style    { return lipgloss.NewStyle().Bold(true) }
func previewMetaStyle() lipgloss.Style     { return helpStyle() }
func previewLabelStyle() lipgloss.Style    { return fg("166", "214") }
func previewReviewerStyle() lipgloss.Style { return fg("27", "39") }

func previewBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().Padding(0, 1)
}

func prNumber(n int) string { return "#" + strconv.Itoa(n) }

func checksDisplay(pr github.PR) (string, string, lipgloss.Style) {
	switch pr.Checks {
	case github.ChecksFail:
		return "✗", "failing", errStyle()
	case github.ChecksPass:
		return "✓", "passing", okStyle()
	default:
		return "-", "no checks", previewMetaStyle()
	}
}

func checksSummary(pr github.PR) string {
	glyph, _, _ := checksDisplay(pr)

	return glyph
}

func mergeLabel(state string) string {
	switch state {
	case mergeClean:
		return "clean"
	case mergeBehind:
		return labelBehind
	case mergeDirty:
		return labelConflict
	case "BLOCKED":
		return labelBlocked
	case "UNSTABLE":
		return "unstable"
	case "DRAFT":
		return "draft"
	case "HAS_HOOKS":
		return "hooks"
	case "UNKNOWN":
		return "unknown"
	default:
		return "-"
	}
}

// mergeSummary counts PRs per non-clean merge state, e.g. "2 behind · 1 conflict".
func mergeSummary(prs []github.PR) string {
	counts := map[string]int{}
	for _, pr := range prs {
		counts[mergeLabel(pr.MergeState)]++
	}

	var parts []string

	for _, label := range []string{labelBehind, labelConflict, labelBlocked, "unstable", "draft", "hooks", "unknown"} {
		if n := counts[label]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}

	return strings.Join(parts, " · ")
}

// selectedRowPrefix is the escape sequence the table opens the cursor row with,
// or "" when colors are off.
func selectedRowPrefix() string {
	prefix, _, _ := strings.Cut(table.DefaultStyles().Selected.Render("|"), "|")

	return prefix
}

func mergeStyle(label string) lipgloss.Style {
	switch label {
	case "clean":
		return okStyle()
	case labelBehind:
		return previewLabelStyle()
	case labelConflict, labelBlocked:
		return errStyle()
	default:
		return previewMetaStyle()
	}
}

// colorMerge colors the Merge column of the rendered table. It works by
// visible offset rather than by text so PR titles that happen to contain
// "behind" or "clean" are never touched.
func colorMerge(rendered string, cols []table.Column) string {
	start, width, found := 1, 0, false // 1: the cell's left padding

	for _, col := range cols {
		if col.Title == "Merge" {
			width, found = col.Width, true

			break
		}

		start += col.Width + cellPadding
	}

	if !found {
		return rendered
	}

	sel := selectedRowPrefix()
	lines := strings.Split(rendered, "\n")

	for idx := 1; idx < len(lines); idx++ { // line 0 is the header
		line := lines[idx]

		cell := strings.TrimSpace(ansi.Strip(ansi.Cut(line, start, start+width)))
		if cell == "" {
			continue
		}

		restore := ""
		if sel != "" && strings.HasPrefix(line, sel) {
			restore = sel
		}

		pad := strings.Repeat(" ", max(width-lipgloss.Width(cell), 0))
		lines[idx] = ansi.Truncate(line, start, "") +
			mergeStyle(cell).Render(cell) + restore + pad +
			ansi.TruncateLeft(line, start+width, "")
	}

	return strings.Join(lines, "\n")
}

// colorChecks colors the check glyphs after the table renders: the table
// truncates cells by width counting ANSI escapes, so cells can't carry color.
// On the cursor row the glyph's reset would end the selection highlight, so
// the highlight is re-opened after each glyph.
func colorChecks(rendered string) string {
	sel := selectedRowPrefix()
	passGlyph, failGlyph := okStyle().Render("✓"), errStyle().Render("✗")

	lines := strings.Split(rendered, "\n")
	for idx, line := range lines {
		restore := ""
		if sel != "" && strings.HasPrefix(line, sel) {
			restore = sel
		}

		lines[idx] = strings.NewReplacer("✓", passGlyph+restore, "✗", failGlyph+restore).
			Replace(line)
	}

	return strings.Join(lines, "\n")
}

// minWidth and minHeight are the smallest terminal the PR table lays out in.
const (
	minWidth  = fixedColsSum + tableOverhead + colTitleMin
	minHeight = previewHeightMargin + minListHeight
)

// tooSmall reports whether the list can't be drawn in the current terminal.
// The size is unknown (0) until the first WindowSizeMsg.
func (m Model) tooSmall() bool {
	return m.screen == screenList && !m.listFits()
}

// listFits reports whether the PR table can be drawn at the current size
// (true while the size is still unknown).
func (m Model) listFits() bool {
	return m.width == 0 || m.height == 0 || (m.width >= minWidth && m.height >= minHeight)
}

// View renders the current screen as a tea.View, satisfying tea.Model. The
// alt screen is always on; mouse mode reflects the --mouse flag (see
// WithMouse).
func (m Model) View() tea.View {
	v := tea.NewView(m.viewString())
	v.AltScreen = true
	v.MouseMode = m.mouseMode

	return v
}

// viewString renders the current screen to a plain string, with every line
// cut to the terminal width so nothing wraps and throws off the height
// accounting.
func (m Model) viewString() string {
	if m.tooSmall() {
		return fit(fmt.Sprintf(
			"terminal too small: need %d×%d, have %d×%d",
			minWidth, minHeight, m.width, m.height,
		), m.width)
	}

	return fit(m.render(), m.width)
}

// fit truncates each line of s to width cells with an ellipsis; width 0
// (size not known yet) leaves s alone.
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}

	return strings.Join(lines, "\n")
}

func (m Model) render() string {
	switch m.screen {
	case screenList:
		return m.viewList(footerStyle().Render(m.footerText()))
	case screenFilter, screenActionInput:
		return m.viewPrompt()
	case screenConfirm:
		return m.viewConfirm()
	case screenResults:
		return m.viewResults()
	case screenHelp:
		return m.viewHelp()
	}

	return ""
}

// viewList draws the header, the list and, on the bottom line, footer: the
// key hints, or the prompt being typed into.
func (m Model) viewList(footer string) string {
	header := headerStyle().Render("gh-bulk-pr") +
		"  " + m.tabBar() + "  " + helpStyle().Render(m.query)

	var body string

	switch {
	case m.err != nil:
		body = header + "\n\n" + errStyle().Render("error: "+m.err.Error())
	default:
		body = header + "\n" + m.statusLine() + "\n" + m.listWithPreview()
	}

	pad := max(m.height-lipgloss.Height(body)-lipgloss.Height(footer)-1, 0)

	return body + strings.Repeat("\n", pad) + "\n" + footer
}

// loadMoreLine is the line under the table: a spinner while a further page is
// being fetched, blank otherwise so the layout doesn't shift when it starts.
func (m Model) loadMoreLine() string {
	if !m.loadingMore {
		return ""
	}

	return " " + m.spinner.View() + helpStyle().Render(" loading more…")
}

func (m Model) listWithPreview() string {
	list := colorMerge(colorChecks(m.table.View()), m.table.Columns()) + "\n" + m.loadMoreLine()

	if pr, ok := m.focusedPR(); ok && m.previewOpen {
		sep := separatorStyle().Render(strings.Repeat("─", max(lipgloss.Width(list), 1)))
		list = lipgloss.JoinVertical(lipgloss.Left, list, sep, previewText(pr))
	}

	return list
}

// promptLabel and promptHint describe the input being typed into on the
// filter and label screens.
func (m Model) promptLabel() string {
	if m.screen == screenActionInput {
		return fmt.Sprintf("Label for %d PR(s): ", len(m.selectedPRs()))
	}

	return "Search: "
}

func (m Model) promptHint() string {
	if m.screen == screenFilter {
		return "enter run · ↑/↓ history · esc cancel"
	}

	if m.screen == screenActionInput {
		return "enter continue · esc cancel"
	}

	return ""
}

// viewPrompt is the filter and label screens: the list stays visible with the
// input on the bottom line, like the / prompt in less and vim. When the list
// can't be drawn at this size it falls back to the bare prompt.
func (m Model) viewPrompt() string {
	input := m.filterInput.View()
	if m.screen == screenActionInput {
		input = m.actionInput.View()
	}

	line := m.promptLabel() + input

	if !m.listFits() {
		return line + "\n" + helpStyle().Render(m.promptHint())
	}

	return m.viewList(lipgloss.NewStyle().Padding(0, 1).Render(line))
}

// statusLine is the line under the header: the loading or refreshing spinner
// on the left, and on the right how many PRs are selected, the merge summary
// and the cursor's position in the results. It is empty when there is nothing
// to say, which keeps the spacer between header and table.
func (m Model) statusLine() string {
	var parts []string

	if hint := m.promptHint(); hint != "" {
		parts = append(parts, helpStyle().Render(hint))
	}

	if len(parts) > 0 {
		return m.spread(m.loadingText(), parts)
	}

	if selected := len(m.selectedPRs()); selected > 0 {
		parts = append(parts, headerStyle().Render(fmt.Sprintf("%d selected", selected)))
	}

	if summary := mergeSummary(m.prs); summary != "" {
		parts = append(parts, helpStyle().Render(summary))
	}

	if position := m.positionText(); position != "" {
		parts = append(parts, helpStyle().Render(position))
	}

	if m.moreErr != nil {
		parts = append(parts, errStyle().Render("load more failed: "+m.moreErr.Error()))
	}

	if m.notice != "" {
		parts = append(parts, errStyle().Render(m.notice))
	}

	return m.spread(m.loadingText(), parts)
}

// loadingText is the spinner shown on the left of the status line while a
// search is in flight: "loading" until there are rows, "refreshing" after.
func (m Model) loadingText() string {
	if !m.loading {
		return ""
	}

	label := " refreshing…"
	if len(m.prs) == 0 {
		label = " loading…"
	}

	return " " + m.spinner.View() + helpStyle().Render(label)
}

// spread puts left at the left edge and parts, joined, at the right edge.
func (m Model) spread(left string, parts []string) string {
	right := strings.Join(parts, "  ")
	if m.width == 0 {
		return strings.TrimSpace(left + "  " + right)
	}

	right = fit(right, max(m.width-lipgloss.Width(left)-1, 0))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)

	return left + strings.Repeat(" ", gap) + right
}

// positionText is the cursor's place in the full result list, e.g.
// "137 of 1482 · 250 loaded". Rows load in sort order, so the cursor index is
// the position among all matches.
func (m Model) positionText() string {
	if len(m.prs) == 0 {
		return ""
	}

	total := max(m.total, len(m.prs))
	text := fmt.Sprintf("%d of %d", m.table.Cursor()+1, total)

	if total > len(m.prs) {
		text += fmt.Sprintf(" · %d loaded", len(m.prs))
	}

	return text
}

// footerPadding is the footer style's horizontal padding.
const footerPadding = 2

// footerText is the key hints for the current state, cut to the terminal
// width instead of wrapping. Once PRs are selected the
// actions come first, after how many are selected.
func (m Model) footerText() string {
	if m.openArmed {
		n := len(m.selectedPRs())

		return fmt.Sprintf(
			"%d selected · press O again to open all %d · any other key cancels",
			n,
			n,
		)
	}

	if m.quitArmed {
		return fmt.Sprintf(
			"%d selected · press q again to quit · any other key cancels",
			len(m.selectedPRs()),
		)
	}

	prefix, bindings := "", m.keys.short()

	if n := len(m.selectedPRs()); n > 0 {
		prefix = fmt.Sprintf("%d selected of %d · ", n, max(m.total, len(m.prs)))
		bindings = m.keys.shortSelected()
	}

	// The help widget only truncates when it has room for its ellipsis, so
	// fit has the last word.
	return fit(prefix+m.help.ShortHelpView(bindings), m.width-footerPadding)
}

func (m Model) viewHelp() string {
	columns := m.help
	columns.SetWidth(m.width)

	return headerStyle().Render("Keys") + "\n\n" + columns.FullHelpView(m.keys.full()) +
		"\n\n" + helpStyle().Render("any key to close")
}

func previewChecks(item github.PR) string {
	glyph, word, style := checksDisplay(item)

	return style.Render(glyph + " " + word)
}

func previewMerge(item github.PR) string {
	label := mergeLabel(item.MergeState)

	return mergeStyle(label).Render("merge: " + label)
}

func previewText(item github.PR) string {
	var buf strings.Builder
	buf.WriteString(previewTitleStyle().Render(item.Title) + "\n")
	fmt.Fprintf(
		&buf,
		"%s · %s · %s\n\n",
		previewMetaStyle().Render(item.Repo+" "+prNumber(item.Number)+" · "+item.Author),
		previewChecks(item),
		previewMerge(item),
	)

	if len(item.Labels) > 0 {
		fmt.Fprintf(
			&buf,
			"%s %s\n",
			previewMetaStyle().Render("labels:"),
			previewLabelStyle().Render(strings.Join(item.Labels, ", ")),
		)
	}

	if len(item.Reviewers) > 0 {
		fmt.Fprintf(
			&buf,
			"%s %s\n",
			previewMetaStyle().Render("reviewers:"),
			previewReviewerStyle().Render(strings.Join(item.Reviewers, ", ")),
		)
	}

	buf.WriteString("\n")

	body := item.Body
	if len(body) > previewBodyLimit {
		body = body[:previewBodyLimit] + "…"
	}

	buf.WriteString(body)

	return previewBoxStyle().Render(buf.String())
}

// paneChrome is the lines around the confirm/results list: the pinned
// heading, the pinned prompt, and one spare.
const paneChrome = 3

// viewPane draws body in the scrolling pane, at its current scroll offset.
// Until the terminal size is known it is drawn whole.
func (m Model) viewPane(body string) string {
	if m.height == 0 {
		return body
	}

	pane := m.pane
	pane.SetContent(body)

	return pane.View()
}

// scrollHint is appended to a prompt when body is taller than the pane.
func (m Model) scrollHint(body string) string {
	if m.height > 0 && strings.Count(body, "\n")+1 > m.pane.Height() {
		return " · j/k/g/G scroll"
	}

	return ""
}

func (m Model) confirmBody() string {
	lines := make([]string, len(m.confirm))
	for i, pr := range m.confirm {
		lines[i] = fmt.Sprintf("  %s %s  %s", pr.Repo, prNumber(pr.Number), pr.Title)
	}

	return strings.Join(lines, "\n")
}

func (m Model) viewConfirm() string {
	confirmKeys := "y/enter"
	if m.action.destructive {
		confirmKeys = "y"
	}

	body := m.confirmBody()
	prompt := fmt.Sprintf("%s to confirm %d PR(s) · n/esc to cancel", confirmKeys, len(m.confirm))

	return fmt.Sprintf("About to %s on %d PR(s):", m.action.label, len(m.confirm)) +
		"\n" + m.viewPane(body) + "\n" + helpStyle().Render(prompt+m.scrollHint(body))
}

// sortedFailuresFirst returns results with failed ones first, otherwise in
// order, so a failure is never scrolled out of sight.
func sortedFailuresFirst(all []worker.Result) []worker.Result {
	sorted := slices.Clone(all)
	slices.SortStableFunc(sorted, func(a, b worker.Result) int {
		return cmp.Compare(boolRank(b.Err != nil), boolRank(a.Err != nil))
	})

	return sorted
}

func boolRank(flag bool) int {
	if flag {
		return 1
	}

	return 0
}

func (m Model) resultsBody() string {
	sorted := sortedFailuresFirst(m.results)
	lines := make([]string, len(sorted))

	for idx, res := range sorted {
		status := okStyle().Render("✓")
		if res.Err != nil {
			status = errStyle().Render("✗ " + res.Err.Error())
		}

		lines[idx] = fmt.Sprintf(
			"  %s %s  %s  %s",
			res.PR.Repo,
			prNumber(res.PR.Number),
			res.PR.Title,
			status,
		)
	}

	return strings.Join(lines, "\n")
}

func (m Model) resultsSummary() string {
	failed := 0

	for _, res := range m.results {
		if res.Err != nil {
			failed++
		}
	}

	summary := fmt.Sprintf("Results: %d ok", len(m.results)-failed)
	if failed > 0 {
		summary += fmt.Sprintf(" · %d failed", failed)
	}

	return summary
}

func (m Model) viewResults() string {
	if m.results == nil {
		return m.viewActionProgress()
	}

	body := m.resultsBody()

	return m.resultsSummary() + "\n" + m.viewPane(body) + "\n" +
		helpStyle().Render("enter/esc to continue"+m.scrollHint(body))
}

func (m Model) viewActionProgress() string {
	done := 0
	if m.actionDone != nil {
		done = int(m.actionDone.Load())
	}

	percent := 0.0
	if m.actionTotal > 0 {
		percent = float64(done) / float64(m.actionTotal)
	}

	return fmt.Sprintf(
		"%s %s %d/%d\n\n%s",
		m.spinner.View(),
		m.action.label,
		done,
		m.actionTotal,
		m.progress.ViewAs(percent),
	)
}

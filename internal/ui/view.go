package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hugoh/gh-bulk-pr/internal/github"
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

func helpStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(lipgloss.Color("241")) }
func errStyle() lipgloss.Style    { return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) }
func okStyle() lipgloss.Style     { return lipgloss.NewStyle().Foreground(lipgloss.Color("42")) }
func headerStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }

func separatorStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color("240")) }

func footerStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 1)
}

func previewTitleStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }

func previewMetaStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color("241")) }

func previewLabelStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color("214")) }

func previewReviewerStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
}

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

// View renders the current screen.
func (m Model) View() string {
	switch m.screen {
	case screenList:
		return m.viewList()
	case screenFilter:
		return "Search: " + m.filterInput.View() + "\n" + helpStyle().Render(
			"enter to run · ↑/↓ history · esc to cancel",
		)
	case screenActionInput:
		return "Label: " + m.actionInput.View() + "\n" + helpStyle().Render(
			"enter to continue · esc to cancel",
		)
	case screenConfirm:
		return m.viewConfirm()
	case screenResults:
		return m.viewResults()
	}

	return ""
}

func (m Model) viewList() string {
	header := headerStyle().Render("gh-bulk-pr") +
		"  " + m.tabBar() + "  " + helpStyle().Render(m.query)

	if m.loading && len(m.prs) == 0 {
		return header + "\n\n" + m.spinner.View() + " loading…"
	}

	if m.err != nil {
		return header + "\n\n" + errStyle().Render("error: "+m.err.Error())
	}

	if summary := mergeSummary(m.prs); summary != "" {
		header += "  " + helpStyle().Render(summary)
	}

	if m.loading {
		header += "  " + m.spinner.View() + helpStyle().Render(" refreshing…")
	}

	list := colorMerge(colorChecks(m.table.View()), m.table.Columns())
	if m.previewOpen {
		if pr, ok := m.focusedPR(); ok {
			sep := separatorStyle().Render(strings.Repeat("─", max(lipgloss.Width(list), 1)))
			list = lipgloss.JoinVertical(lipgloss.Left, list, sep, previewText(pr))
		}
	}

	body := header + "\n\n" + list
	footer := footerStyle().Render(m.footerText())

	pad := max(m.height-lipgloss.Height(body)-lipgloss.Height(footer)-1, 0)

	return body + strings.Repeat("\n", pad) + "\n" + footer
}

func (m Model) footerText() string {
	if n := len(m.selectedPRs()); n > 0 {
		return fmt.Sprintf("%d selected · [l]abel  [c]lose  [m]erge  [r]efresh  [Esc] clear", n)
	}

	return "j/k move · x select · Enter/p preview · T checks · / filter · 1/2 tab · Ctrl+a all · r refresh · q quit"
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

func (m Model) viewConfirm() string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "About to %s on %d PR(s):\n\n", m.action.label, len(m.confirm))

	for _, pr := range m.confirm {
		fmt.Fprintf(&buf, "  %s %s  %s\n", pr.Repo, prNumber(pr.Number), pr.Title)
	}

	buf.WriteString("\n" + helpStyle().Render("y/enter to confirm · n/esc to cancel"))

	return buf.String()
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

func (m Model) viewResults() string {
	if m.results == nil {
		return m.viewActionProgress()
	}

	var buf strings.Builder
	buf.WriteString("Results:\n\n")

	for _, res := range m.results {
		status := okStyle().Render("✓")
		if res.Err != nil {
			status = errStyle().Render("✗ " + res.Err.Error())
		}

		fmt.Fprintf(
			&buf,
			"  %s %s  %s  %s\n",
			res.PR.Repo,
			prNumber(res.PR.Number),
			res.PR.Title,
			status,
		)
	}

	buf.WriteString("\n" + helpStyle().Render("enter/esc to continue"))

	return buf.String()
}

package ui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

// GitHub Primer roles: the light and dark default themes. The terminal
// background is detected by compat.AdaptiveColor.
func colorBase() compat.AdaptiveColor    { return adaptive("#ffffff", "#0d1117") }
func colorSurface() compat.AdaptiveColor { return adaptive("#eaeef2", "#30363d") }
func colorOverlay() compat.AdaptiveColor { return adaptive("#818b98", "#6e7681") }
func colorMuted() compat.AdaptiveColor   { return adaptive("#59636e", "#9198a1") }
func colorAccent() compat.AdaptiveColor  { return adaptive("#0969da", "#4493f8") }
func colorInfo() compat.AdaptiveColor    { return adaptive("#8250df", "#ab7df8") }
func colorOK() compat.AdaptiveColor      { return adaptive("#1a7f37", "#3fb950") }
func colorWarn() compat.AdaptiveColor    { return adaptive("#bc4c00", "#db6d28") }
func colorErr() compat.AdaptiveColor     { return adaptive("#d1242f", "#f85149") }

// adaptive is a colour that reads on both light and dark terminal backgrounds.
func adaptive(light, dark string) compat.AdaptiveColor {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}

func fg(c compat.AdaptiveColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func helpStyle() lipgloss.Style            { return fg(colorMuted()) }
func errStyle() lipgloss.Style             { return fg(colorErr()) }
func okStyle() lipgloss.Style              { return fg(colorOK()) }
func headerStyle() lipgloss.Style          { return fg(colorAccent()).Bold(true) }
func separatorStyle() lipgloss.Style       { return fg(colorOverlay()) }
func footerStyle() lipgloss.Style          { return helpStyle().Padding(0, 1) }
func previewTitleStyle() lipgloss.Style    { return lipgloss.NewStyle().Bold(true) }
func previewMetaStyle() lipgloss.Style     { return helpStyle() }
func previewLabelStyle() lipgloss.Style    { return fg(colorWarn()) }
func previewReviewerStyle() lipgloss.Style { return fg(colorInfo()) }

func previewBoxStyle() lipgloss.Style { return lipgloss.NewStyle().Padding(0, 1) }

func activeTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(colorBase()).Background(colorAccent())
}

func tableStyles() table.Styles {
	return table.Styles{
		Selected: lipgloss.NewStyle().Bold(true).Background(colorSurface()),
		Header:   fg(colorAccent()).Bold(true).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
	}
}

func helpStyles() help.Styles {
	key, desc, sep := fg(colorInfo()), fg(colorMuted()), fg(colorOverlay())

	return help.Styles{
		Ellipsis:       sep,
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
}

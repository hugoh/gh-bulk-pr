package ui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpScreen_OpensListsEveryKeyAndCloses(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m, _ = m.handleListKeyByString("?")
	require.Equal(t, screenHelp, m.screen)

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{
		"↑/k", "↓/j", "g/home", "G/end", "pgup/b", "pgdn/f", // navigation, handled by the table
		"x/space", "ctrl+a", "esc", "enter/p", // selecting and previewing
		"/", "open/closed", "1/2", "r", "T", "o", "open", "O", "open all", // searching
		textLabel, actionClose, "merge", textAuto, "select all", "checks", textHelp, "quit",
	} {
		assert.Contains(t, view, want)
	}

	closed, cmd := m.handleKey(keyMsgFromString("x"))
	assert.Equal(t, screenList, closed.screen, "any key closes the help")
	assert.Nil(t, cmd)
	assert.Empty(t, closed.selected, "the key that closed it isn't also applied")
}

func TestFooter_ShortHelp(t *testing.T) {
	t.Parallel()

	footer := footerStyle().Render(loadedModel().footerText())

	assert.LessOrEqual(t, lipgloss.Width(footer), 100)

	plain := ansi.Strip(footer)
	for _, want := range []string{"move", "select", "preview", "o open", textFilter, "refresh", "? help", "q quit"} {
		assert.Contains(t, plain, want)
	}
}

func TestFooter_TruncatesInsteadOfWrapping(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.width = 60

	assert.LessOrEqual(t, lipgloss.Width(footerStyle().Render(m.footerText())), 60)

	m.selected[keyOf(m.prs[0])] = true

	assert.LessOrEqual(t, lipgloss.Width(footerStyle().Render(m.footerText())), 60)
}

func TestFooter_SelectionShowsTheActions(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	assert.NotContains(t, m.footerText(), "merge")

	m.selected[keyOf(m.prs[0])] = true
	footer := m.footerText()

	assert.Contains(t, footer, "1 selected of 2")

	plain := ansi.Strip(footer)
	for _, want := range []string{textLabel, actionClose, "merge", textAuto, "open all", "clear", "? help"} {
		assert.Contains(t, plain, want)
	}

	assert.NotContains(t, footer, "preview")
}

func TestKeyMap_EveryDispatchedBindingHasKeysAndHelp(t *testing.T) {
	t.Parallel()

	keys := newKeyMap()

	for name, group := range map[string][]string{
		textShort:  helpTexts(keys.short()),
		"selected": helpTexts(keys.shortSelected()),
	} {
		assert.NotEmpty(t, group, name)
	}

	for _, column := range keys.full() {
		for _, binding := range column {
			assert.NotEmpty(t, binding.Keys(), binding.Help().Desc)
			assert.NotEmpty(t, binding.Help().Key)
			assert.NotEmpty(t, binding.Help().Desc)
		}
	}
}

func helpTexts(bindings []key.Binding) []string {
	texts := make([]string, len(bindings))
	for i, binding := range bindings {
		texts[i] = binding.Help().Key + " " + binding.Help().Desc
	}

	return texts
}

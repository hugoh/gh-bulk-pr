package ui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	baseQuery     = "is:open is:pr archived:false sort:updated-desc"
	involvesQuery = baseQuery + " involves:@me"
	ownerQuery    = baseQuery + " owner:@me"

	// DefaultQuery is the first tab's query.
	DefaultQuery = involvesQuery
	noTab        = -1
)

type tab struct{ name, query string }

func tabs() []tab {
	return []tab{{"involves:@me", involvesQuery}, {"owner:@me", ownerQuery}}
}

const (
	stateOpen   = "is:open"
	stateClosed = "is:closed"
)

// flipState swaps is:open and is:closed in query, and reports whether it
// found either. Extra whitespace is collapsed.
func flipState(query string) (string, bool) {
	words := strings.Fields(query)
	found := false

	for i, word := range words {
		switch word {
		case stateOpen:
			words[i], found = stateClosed, true
		case stateClosed:
			words[i], found = stateOpen, true
		}
	}

	return strings.Join(words, " "), found
}

func isClosed(query string) bool {
	return slices.Contains(strings.Fields(query), stateClosed)
}

// tabFor returns the tab whose query equals query, ignoring extra whitespace
// and whether it asks for open or closed PRs, or noTab.
func tabFor(query string) int {
	normalized := strings.Join(strings.Fields(query), " ")
	if isClosed(normalized) {
		normalized, _ = flipState(normalized)
	}

	for i, tab := range tabs() {
		if tab.query == normalized {
			return i
		}
	}

	return noTab
}

func (m Model) tabBar() string {
	parts := make([]string, len(tabs()))

	for idx, tab := range tabs() {
		label := strconv.Itoa(idx+1) + " " + tab.name

		if idx != m.tab {
			parts[idx] = helpStyle().Render(" " + label + " ")

			continue
		}

		if isClosed(m.query) {
			label += " (closed)"
		}

		parts[idx] = activeTabStyle().Render(" " + label + " ")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

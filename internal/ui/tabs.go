package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// tabFor returns the tab whose query equals query, ignoring extra whitespace,
// or noTab.
func tabFor(query string) int {
	normalized := strings.Join(strings.Fields(query), " ")

	for i, tab := range tabs() {
		if tab.query == normalized {
			return i
		}
	}

	return noTab
}

func (m Model) tabBar() string {
	parts := make([]string, len(tabs()))

	for i, tab := range tabs() {
		label := strconv.Itoa(i+1) + " " + tab.name
		if i == m.tab {
			parts[i] = headerStyle().Reverse(true).Render(" " + label + " ")
		} else {
			parts[i] = helpStyle().Render(" " + label + " ")
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

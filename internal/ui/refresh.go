package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// maxRefreshInterval caps the backoff while polls find nothing new.
	maxRefreshInterval = 5 * time.Minute
	// maxPollBatch caps how many PRs one poll re-fetches.
	maxPollBatch = 50
	// backoffFactor is how much slower each fruitless poll makes the next.
	backoffFactor = 2
)

// autoRefreshMsg fires when it is time to re-fetch the PRs still waiting on
// something.
type autoRefreshMsg struct{}

// WithRefresh sets how often PRs whose checks are still pending are
// re-fetched. Zero or less turns polling off.
func (m Model) WithRefresh(every time.Duration) Model {
	m.refreshEvery = max(every, 0)
	m.refreshInterval = m.refreshEvery

	return m
}

// waitingIDs lists the loaded PRs whose state can still change by itself.
func (m Model) waitingIDs() []string {
	var ids []string

	for _, pr := range m.prs {
		if pr.Waiting() {
			ids = append(ids, pr.ID)
		}
	}

	return ids
}

// scheduleRefresh arms the next poll, unless polling is off, one is already
// armed, or nothing is waiting.
func (m Model) scheduleRefresh() (Model, tea.Cmd) {
	if m.refreshEvery <= 0 || m.refreshArmed || len(m.waitingIDs()) == 0 {
		return m, nil
	}

	m.refreshArmed = true

	return m, tea.Tick(m.refreshInterval, func(time.Time) tea.Msg { return autoRefreshMsg{} })
}

// handleAutoRefresh re-fetches the waiting PRs, unless a search is running or
// the list isn't the screen in front, then arms the next poll.
func (m Model) handleAutoRefresh() (Model, tea.Cmd) {
	m.refreshArmed = false

	var fetch tea.Cmd

	if m.screen == screenList && !m.loading {
		m, fetch = m.fetchIDs(slices.Clone(m.waitingIDs())[:min(len(m.waitingIDs()), maxPollBatch)])
	}

	m, next := m.scheduleRefresh()

	return m, tea.Batch(fetch, next)
}

// backOff slows polling after a poll that found nothing new or failed.
func (m Model) backOff() Model {
	m.refreshInterval = min(m.refreshInterval*backoffFactor, maxRefreshInterval)

	return m
}

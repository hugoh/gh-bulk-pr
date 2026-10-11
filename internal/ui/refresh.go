package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

const (
	// maxRefreshInterval caps the backoff of a PR whose polls find nothing new.
	maxRefreshInterval = 5 * time.Minute
	// maxPollBatch caps how many PRs one poll re-fetches.
	maxPollBatch = 50
	// backoffFactor is how much slower each fruitless poll of a PR makes the next.
	backoffFactor = 2
	// pollTick is how often due polls are looked for, at most.
	pollTick = 5 * time.Second
)

// autoRefreshMsg fires when it is time to look for polls that are due.
type autoRefreshMsg struct{}

// poll is when a PR waiting on something is next re-fetched. Each PR keeps
// its own pace, so one that never settles doesn't hold up the others.
type poll struct {
	next     time.Time
	interval time.Duration
}

// WithRefresh sets how often PRs that are waiting on something are
// re-fetched. Zero or less turns polling off.
func (m Model) WithRefresh(every time.Duration) Model {
	m.refreshEvery = max(every, 0)

	return m
}

// schedulePolls sets the next poll of each of ids that is listed and waiting,
// and drops the others: at the base rate when changed says its details moved,
// backing off when they didn't.
func (m Model) schedulePolls(ids []string, changed map[string]bool, now time.Time) Model {
	listed := make(map[string]github.PR, len(m.prs))
	for _, pull := range m.prs {
		listed[pull.ID] = pull
	}

	for _, prID := range ids {
		pull, ok := listed[prID]
		if !ok || !pull.Waiting() || m.refreshEvery <= 0 {
			delete(m.polls, prID)

			continue
		}

		next := m.polls[prID]
		if changed[prID] || next.interval == 0 {
			next.interval = m.refreshEvery
		} else {
			next.interval = min(next.interval*backoffFactor, maxRefreshInterval)
		}

		next.next = now.Add(next.interval)
		m.polls[prID] = next
	}

	return m
}

// detailsChanged reports which of details differ from the last known ones in
// what a poll waits on.
func (m Model) detailsChanged(details []github.Detail) map[string]bool {
	changed := map[string]bool{}

	for _, detail := range details {
		old, known := m.details[detail.ID]
		changed[detail.ID] = !known || old.Checks != detail.Checks ||
			old.MergeState != detail.MergeState ||
			old.AutoMerge != detail.AutoMerge ||
			!slices.Equal(old.Pending, detail.Pending) ||
			github.PR{Body: old.Body, Labels: old.Labels}.RebaseRequested() !=
				github.PR{Body: detail.Body, Labels: detail.Labels}.RebaseRequested()
	}

	return changed
}

// armPoll schedules the next look for due polls, unless one is scheduled or
// nothing is polled.
func (m Model) armPoll() (Model, tea.Cmd) {
	if m.pollArmed || len(m.polls) == 0 {
		return m, nil
	}

	m.pollArmed = true

	return m, tea.Tick(
		min(m.refreshEvery, pollTick),
		func(time.Time) tea.Msg { return autoRefreshMsg{} },
	)
}

// handleAutoRefresh forgets the polls of PRs no longer listed, re-fetches the
// due ones unless a search is running or the list isn't the screen in front,
// then arms the next look.
func (m Model) handleAutoRefresh() (Model, tea.Cmd) {
	m.pollArmed = false

	listed := make(map[string]bool, len(m.prs))
	for _, pull := range m.prs {
		listed[pull.ID] = true
	}

	for id := range m.polls {
		if !listed[id] {
			delete(m.polls, id)
		}
	}

	var fetch tea.Cmd

	if m.screen == screenList && !m.loading {
		m, fetch = m.fetchIDs(m.dueIDs(time.Now()))
	}

	m, next := m.armPoll()

	return m, tea.Batch(fetch, next)
}

// dueIDs lists, in list order, the PRs whose poll is due and not in flight.
func (m Model) dueIDs(now time.Time) []string {
	var ids []string

	for _, pull := range m.prs {
		next, polled := m.polls[pull.ID]
		if polled && !next.next.After(now) && m.fetched[pull.ID] != fetchInflight {
			ids = append(ids, pull.ID)
		}

		if len(ids) == maxPollBatch {
			break
		}
	}

	return ids
}

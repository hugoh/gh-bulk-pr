package ui

import (
	"testing"
	"time"

	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRefresh = 30 * time.Second

func pendingDetail(number int) github.Detail {
	detail := detailFor(number)
	detail.Checks = github.ChecksPending
	detail.Pending = []string{"renovate/stability-days"}

	return detail
}

// refreshModel has PRs 1 and 2 pending and the rest passing, all fetched.
func refreshModel(t *testing.T) Model {
	t.Helper()

	m := detailModel(t).WithRefresh(testRefresh)
	ids := make([]string, 0, 50)
	details := make([]github.Detail, 0, 50)

	for number := 1; number <= 50; number++ {
		ids = append(ids, idOf(number))
		if number <= 2 {
			details = append(details, pendingDetail(number))
		} else {
			details = append(details, detailFor(number))
		}
	}

	m, _ = m.handleDetailsDone(detailsDoneMsg{ids: ids, details: details})
	m.fetching = 0

	return m
}

// due makes the polls of ids fall due.
func due(m Model, ids ...string) Model {
	for _, id := range ids {
		next := m.polls[id]
		next.next = time.Now().Add(-time.Second)
		m.polls[id] = next
	}

	return m
}

func TestHandleDetailsDone_PollsEachWaitingPR(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)

	require.Len(t, m.polls, 2)
	assert.Equal(t, testRefresh, m.polls[idOf(1)].interval)
	assert.Equal(t, testRefresh, m.polls[idOf(2)].interval)
	assert.True(t, m.pollArmed)
}

func TestHandleDetailsDone_PollsAutoMergingPRs(t *testing.T) {
	t.Parallel()

	m := detailModel(t).WithRefresh(testRefresh)
	auto := detailFor(1)
	auto.AutoMerge = true

	m, _ = m.handleDetailsDone(
		detailsDoneMsg{ids: []string{idOf(1)}, details: []github.Detail{auto}},
	)

	assert.Contains(t, m.polls, idOf(1), "auto-merge closes it without anyone acting")
}

func TestHandleDetailsDone_NoPollWhenDisabled(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		ids: []string{idOf(1)}, details: []github.Detail{pendingDetail(1)},
	})

	assert.Empty(t, m.polls)
	assert.False(t, m.pollArmed)
}

func TestHandleAutoRefresh_RefetchesOnlyDuePRs(t *testing.T) {
	t.Parallel()

	m := due(refreshModel(t), idOf(1))

	m, cmd := m.handleAutoRefresh()

	assert.NotNil(t, cmd)
	assert.Equal(t, fetchInflight, m.fetched[idOf(1)])
	assert.Equal(t, fetchDone, m.fetched[idOf(2)], "not due yet")
	assert.Equal(t, fetchDone, m.fetched[idOf(3)])
	assert.Equal(t, 1, m.fetching)
}

func TestHandleAutoRefresh_SkipsWhileSearching(t *testing.T) {
	t.Parallel()

	m := due(refreshModel(t), idOf(1))
	m.loading = true

	m, _ = m.handleAutoRefresh()

	assert.Equal(t, fetchDone, m.fetched[idOf(1)])
	assert.True(t, m.pollArmed, "it keeps ticking")
}

func TestHandleAutoRefresh_SkipsOffTheListScreen(t *testing.T) {
	t.Parallel()

	m := due(refreshModel(t), idOf(1))
	m.screen = screenConfirm

	m, _ = m.handleAutoRefresh()

	assert.Equal(t, fetchDone, m.fetched[idOf(1)])
}

func TestHandleAutoRefresh_ForgetsPRsNoLongerListed(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.prs = m.prs[1:]

	m, _ = m.handleAutoRefresh()

	assert.NotContains(t, m.polls, idOf(1))
	assert.Contains(t, m.polls, idOf(2))
}

func TestHandleDetailsDone_StopsPollingWhenNothingWaits(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		ids:     []string{idOf(1), idOf(2)},
		details: []github.Detail{detailFor(1), detailFor(2)},
	})
	m.pollArmed = false

	m, cmd := m.handleAutoRefresh()

	assert.Empty(t, m.polls)
	assert.Nil(t, cmd)
	assert.False(t, m.pollArmed)
}

func TestHandleDetailsDone_PollIntervals(t *testing.T) {
	t.Parallel()

	changed := pendingDetail(1)
	changed.Pending = []string{"build"}

	rebaseLabeled := pendingDetail(1)
	rebaseLabeled.Labels = []string{"rebase"}

	tests := []struct {
		name   string
		start  time.Duration
		detail github.Detail
		err    error
		want   time.Duration
	}{
		{"unchanged backs off", testRefresh, pendingDetail(1), nil, 2 * testRefresh},
		{"backoff is capped", maxRefreshInterval, pendingDetail(1), nil, maxRefreshInterval},
		{"a change resets it", 4 * testRefresh, changed, nil, testRefresh},
		{"a rebase label resets it", 4 * testRefresh, rebaseLabeled, nil, testRefresh},
		{"a failure backs off", testRefresh, github.Detail{}, assert.AnError, 2 * testRefresh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := refreshModel(t)
			m.polls[idOf(1)] = poll{interval: tt.start}
			msg := detailsDoneMsg{ids: []string{idOf(1)}, err: tt.err}

			if tt.err == nil {
				msg.details = []github.Detail{tt.detail}
			}

			m, _ = m.handleDetailsDone(msg)

			assert.Equal(t, tt.want, m.polls[idOf(1)].interval)
			assert.Equal(t, testRefresh, m.polls[idOf(2)].interval, "other PRs keep their own pace")
		})
	}
}

func TestWithRefresh_NonPositiveDisables(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").WithRefresh(-time.Second)

	require.Zero(t, m.refreshEvery)
}

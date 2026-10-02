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

	m, _ = m.handleDetailsDone(detailsDoneMsg{searchID: m.searchID, ids: ids, details: details})
	m.fetching = 0

	return m
}

func TestWaitingIDs_OnlyPendingPRs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{idOf(1), idOf(2)}, refreshModel(t).waitingIDs())
}

func TestHandleDetailsDone_SchedulesPollWhileSomethingIsPending(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)

	assert.True(t, m.refreshArmed)
}

func TestHandleDetailsDone_NoPollWhenDisabled(t *testing.T) {
	t.Parallel()

	m := detailModel(t)
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID, ids: []string{idOf(1)}, details: []github.Detail{pendingDetail(1)},
	})

	assert.False(t, m.refreshArmed)
}

func TestHandleAutoRefresh_RefetchesPendingPRs(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)

	m, cmd := m.handleAutoRefresh()

	assert.NotNil(t, cmd)
	assert.Equal(t, fetchInflight, m.fetched[idOf(1)])
	assert.Equal(t, fetchInflight, m.fetched[idOf(2)])
	assert.Equal(t, fetchDone, m.fetched[idOf(3)])
	assert.Equal(t, 1, m.fetching)
}

func TestHandleAutoRefresh_SkipsWhileSearching(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.loading = true

	m, _ = m.handleAutoRefresh()

	assert.Equal(t, fetchDone, m.fetched[idOf(1)])
	assert.True(t, m.refreshArmed, "it keeps ticking")
}

func TestHandleAutoRefresh_SkipsOffTheListScreen(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.screen = screenConfirm

	m, _ = m.handleAutoRefresh()

	assert.Equal(t, fetchDone, m.fetched[idOf(1)])
}

func TestHandleAutoRefresh_StopsWhenNothingIsPending(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.details[idOf(1)] = detailFor(1)
	m.details[idOf(2)] = detailFor(2)
	m = m.withStoredDetails()

	m, _ = m.handleAutoRefresh()

	assert.False(t, m.refreshArmed)
}

func TestHandleDetailsDone_BacksOffWhenNothingChanged(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m, _ = m.handleAutoRefresh()
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID,
		ids:      []string{idOf(1), idOf(2)},
		details:  []github.Detail{pendingDetail(1), pendingDetail(2)},
	})

	assert.Equal(t, 2*testRefresh, m.refreshInterval)
}

func TestHandleDetailsDone_BackoffIsCapped(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.refreshInterval = maxRefreshInterval
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID, ids: []string{idOf(1)}, details: []github.Detail{pendingDetail(1)},
	})

	assert.Equal(t, maxRefreshInterval, m.refreshInterval)
}

func TestHandleDetailsDone_ChangeResetsInterval(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.refreshInterval = 4 * testRefresh
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID, ids: []string{idOf(1)}, details: []github.Detail{detailFor(1)},
	})

	assert.Equal(t, testRefresh, m.refreshInterval)
	assert.Equal(t, github.ChecksPass, m.prs[0].Checks)
}

func TestHandleDetailsDone_FailureBacksOff(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m, _ = m.handleDetailsDone(detailsDoneMsg{
		searchID: m.searchID, ids: []string{idOf(1)}, err: assert.AnError,
	})

	assert.Equal(t, 2*testRefresh, m.refreshInterval)
}

func TestReload_ResetsInterval(t *testing.T) {
	t.Parallel()

	m := refreshModel(t)
	m.refreshInterval = 8 * testRefresh

	m, _ = m.reload()

	assert.Equal(t, testRefresh, m.refreshInterval)
}

func TestWithRefresh_NonPositiveDisables(t *testing.T) {
	t.Parallel()

	m := New(nil, "q").WithRefresh(-time.Second)

	require.Zero(t, m.refreshEvery)
}

package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}

func newTestRESTClient(t *testing.T, rt roundTripFunc) *api.RESTClient {
	t.Helper()

	client, err := api.NewRESTClient(
		api.ClientOptions{Host: "github.com", AuthToken: "test", Transport: rt},
	)
	require.NoError(t, err)

	return client
}

func newTestGraphQLClient(t *testing.T, rt roundTripFunc) *api.GraphQLClient {
	t.Helper()

	client, err := api.NewGraphQLClient(
		api.ClientOptions{Host: "github.com", AuthToken: "test", Transport: rt},
	)
	require.NoError(t, err)

	return client
}

const (
	testRepo   = "hugoh/gh-bulk-pr"
	testNodeID = "PR_1"
)

const searchResponseJSON = `{
  "data": {
    "search": {
      "issueCount": 312,
      "pageInfo": {"hasNextPage": %t, "endCursor": "%s"},
      "nodes": [
        {
          "id": "PR_node1",
          "number": 1,
          "title": "Fix bug",
          "autoMergeRequest": {"enabledAt": "2026-09-20T00:00:00Z"},
          "url": "https://github.com/hugoh/r/pull/1",
          "repository": {
            "nameWithOwner": "hugoh/r",
            "squashMergeAllowed": false, "mergeCommitAllowed": false, "rebaseMergeAllowed": true
          },
          "author": {"login": "hugoh"}
        }
      ]
    }
  }
}`

func graphQLCapturing(t *testing.T, sent *string, response string) *api.GraphQLClient {
	t.Helper()

	return newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		*sent = string(body)

		return jsonResponse(response), nil
	})
}

func TestSearchPage(t *testing.T) {
	t.Parallel()

	var sent string

	gql := graphQLCapturing(t, &sent, fmt.Sprintf(searchResponseJSON, true, "cursor2"))

	page, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "cursor1")
	require.NoError(t, err)

	assert.Contains(t, sent, `"after":"cursor1"`)
	assert.Equal(t, 312, page.Total)
	assert.Equal(t, "cursor2", page.EndCursor)
	assert.True(t, page.HasNext)
	require.Len(t, page.PRs, 1)

	pr := page.PRs[0]
	assert.Equal(t, 1, pr.Number)
	assert.Equal(t, "Fix bug", pr.Title)
	assert.Equal(t, "hugoh/r", pr.Repo)
	assert.Equal(t, "hugoh", pr.Author)
	assert.Equal(t, "PR_node1", pr.ID)
	assert.True(t, pr.AutoMerge, "auto-merge is cheap, so the search carries it")
	assert.Equal(t, MergeRebase, pr.MergeMethod, "the only method the repo allows")
	assert.False(t, pr.Detailed)
}

func TestSearchPage_SkipsTheSlowFields(t *testing.T) {
	t.Parallel()

	var sent string

	gql := graphQLCapturing(t, &sent, fmt.Sprintf(searchResponseJSON, false, ""))

	_, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "")
	require.NoError(t, err)

	for _, field := range []string{"mergeStateStatus", "statusCheckRollup", "reviewRequests", "labels", "body"} {
		assert.NotContains(t, sent, field, "Details fetches it, batched")
	}
}

func TestSearchPage_FirstPageSendsNullCursor(t *testing.T) {
	t.Parallel()

	var sent string

	gql := graphQLCapturing(t, &sent, fmt.Sprintf(searchResponseJSON, false, ""))

	page, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "")
	require.NoError(t, err)

	assert.Contains(t, sent, `"after":null`)
	assert.False(t, page.HasNext)
}

func TestSearchPage_TransportError(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})

	_, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "")
	require.Error(t, err)
}

const detailsResponseJSON = `{
  "data": {
    "nodes": [
      {
        "id": "PR_node1",
        "state": "OPEN",
        "autoMergeRequest": {"enabledAt": "2026-10-11T15:00:00Z"},
        "body": "body",
        "mergeStateStatus": "BEHIND",
        "labels": {"nodes": [{"name": "bug"}]},
        "reviewRequests": {"nodes": [
          {"requestedReviewer": {"login": "alice"}},
          {"requestedReviewer": {"name": "core-team"}}
        ]},
        "commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "SUCCESS"}}}]}
      },
      null,
      {
        "id": "PR_node3",
        "state": "MERGED",
        "closedAt": "2026-10-11T15:55:34Z",
        "mergeStateStatus": "DIRTY",
        "commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "ERROR"}}}]}
      }
    ]
  }
}`

func TestDetails(t *testing.T) {
	t.Parallel()

	var sent string

	gql := graphQLCapturing(t, &sent, detailsResponseJSON)

	details, err := (&Client{gql: gql}).Details(
		context.Background(),
		[]string{"PR_node1", "PR_gone", "PR_node3"},
	)
	require.NoError(t, err)

	assert.Contains(t, sent, `"ids":["PR_node1","PR_gone","PR_node3"]`)
	require.Len(t, details, 2, "an ID that no longer resolves is skipped")

	assert.Equal(t, Detail{
		ID:         "PR_node1",
		Body:       "body",
		AutoMerge:  true,
		MergeState: "BEHIND",
		Checks:     ChecksPass,
		Labels:     []string{"bug"},
		Reviewers:  []string{"alice", "core-team"},
	}, details[0])
	assert.Equal(t, ChecksFail, details[1].Checks)
	assert.Equal(t, "DIRTY", details[1].MergeState)
	assert.True(t, details[1].Closed, "a merged PR is closed")
	assert.Equal(t, time.Date(2026, 10, 11, 15, 55, 34, 0, time.UTC), details[1].ClosedAt)
}

func TestDetails_NoIDsSendsNothing(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		t.Error("no request expected")

		return jsonResponse(`{"data": {}}`), nil
	})

	details, err := (&Client{gql: gql}).Details(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, details)
}

func TestDetails_TransportError(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})

	_, err := (&Client{gql: gql}).Details(context.Background(), []string{testNodeID})
	require.ErrorContains(t, err, "boom")
}

const stabilityDays = "renovate/stability-days"

const pendingDetailsResponseJSON = `{
  "data": {
    "nodes": [
      {
        "id": "PR_wait",
        "mergeStateStatus": "BLOCKED",
        "commits": {"nodes": [{"commit": {"statusCheckRollup": {
          "state": "PENDING",
          "contexts": {"nodes": [
            {"context": "renovate/stability-days", "state": "PENDING"},
            {"context": "ci/lint", "state": "SUCCESS"},
            {"name": "build", "status": "IN_PROGRESS", "conclusion": ""},
            {"name": "test", "status": "COMPLETED", "conclusion": "SUCCESS"}
          ]}
        }}}]}
      }
    ]
  }
}`

func TestDetails_PendingChecks(t *testing.T) {
	t.Parallel()

	var sent string

	gql := graphQLCapturing(t, &sent, pendingDetailsResponseJSON)

	details, err := (&Client{gql: gql}).Details(context.Background(), []string{"PR_wait"})
	require.NoError(t, err)
	require.Len(t, details, 1)

	assert.Contains(t, sent, "contexts")
	assert.Equal(t, ChecksPending, details[0].Checks)
	assert.Equal(t, []string{stabilityDays, "build"}, details[0].Pending)
}

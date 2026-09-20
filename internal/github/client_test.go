package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

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

func TestClientActions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		call           func(context.Context, *Client) error
		wantMethod     string
		wantPath       string
		wantBodySubstr string
	}{
		"add label": {
			call: func(ctx context.Context, c *Client) error {
				return c.AddLabel(ctx, PR{Repo: testRepo, Number: 7}, "bug")
			},
			wantMethod:     http.MethodPost,
			wantPath:       "/repos/hugoh/gh-bulk-pr/issues/7/labels",
			wantBodySubstr: `"bug"`,
		},
		"close pr": {
			call:           func(ctx context.Context, c *Client) error { return c.ClosePR(ctx, PR{Repo: testRepo, Number: 7}) },
			wantMethod:     http.MethodPatch,
			wantPath:       "/repos/hugoh/gh-bulk-pr/pulls/7",
			wantBodySubstr: `"closed"`,
		},
		"merge pr": {
			call:           func(ctx context.Context, c *Client) error { return c.MergePR(ctx, PR{Repo: testRepo, Number: 7}) },
			wantMethod:     http.MethodPut,
			wantPath:       "/repos/hugoh/gh-bulk-pr/pulls/7/merge",
			wantBodySubstr: `"squash"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string

			var gotBody bytes.Buffer

			rest := newTestRESTClient(t, func(r *http.Request) (*http.Response, error) {
				gotMethod = r.Method
				gotPath = r.URL.Path

				if r.Body != nil {
					_, _ = io.Copy(&gotBody, r.Body)
				}

				return jsonResponse("{}"), nil
			})

			client := &Client{rest: rest}
			require.NoError(t, tt.call(context.Background(), client))

			assert.Equal(t, tt.wantMethod, gotMethod)
			assert.Equal(t, tt.wantPath, gotPath)
			assert.Contains(t, gotBody.String(), tt.wantBodySubstr)
		})
	}
}

func TestClientActions_TransportError(t *testing.T) {
	t.Parallel()

	rest := newTestRESTClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})

	client := &Client{rest: rest}

	tests := map[string]func() error{
		"add label": func() error { return client.AddLabel(context.Background(), PR{Repo: testRepo, Number: 1}, "bug") },
		"close pr":  func() error { return client.ClosePR(context.Background(), PR{Repo: testRepo, Number: 1}) },
		"merge pr":  func() error { return client.MergePR(context.Background(), PR{Repo: testRepo, Number: 1}) },
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, call())
		})
	}
}

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
          "repository": {"nameWithOwner": "hugoh/r"},
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
		MergeState: "BEHIND",
		Checks:     ChecksPass,
		Labels:     []string{"bug"},
		Reviewers:  []string{"alice", "core-team"},
	}, details[0])
	assert.Equal(t, ChecksFail, details[1].Checks)
	assert.Equal(t, "DIRTY", details[1].MergeState)
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

func TestPR_WithDetail(t *testing.T) {
	t.Parallel()

	light := PR{ID: "PR_1", Number: 3, Title: "Fix", AutoMerge: true}
	full := light.WithDetail(Detail{
		ID: "PR_1", Body: "b", MergeState: MergeClean, Checks: ChecksPass,
		Labels: []string{"x"}, Reviewers: []string{"y"},
	})

	assert.True(t, full.Detailed)
	assert.Equal(t, "Fix", full.Title, "the search's fields are kept")
	assert.True(t, full.AutoMerge)
	assert.Equal(t, "b", full.Body)
	assert.Equal(t, MergeClean, full.MergeState)
	assert.Equal(t, ChecksPass, full.Checks)
	assert.Equal(t, []string{"x"}, full.Labels)
	assert.Equal(t, []string{"y"}, full.Reviewers)
	assert.False(t, light.Detailed, "the original is untouched")
}

func TestToggleAutoMerge(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr        PR
		wantQuery string
		wantVars  string
	}{
		"enables when off": {
			pr:        PR{ID: testNodeID},
			wantQuery: "enablePullRequestAutoMerge",
			wantVars:  "mergeMethod: SQUASH",
		},
		"disables when on": {
			pr:        PR{ID: testNodeID, AutoMerge: true},
			wantQuery: "disablePullRequestAutoMerge",
			wantVars:  "disablePullRequestAutoMerge(input: {pullRequestId: $id})",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var sent string

			gql := newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(r.Body)
				sent = string(body)

				return jsonResponse(`{"data": {}}`), nil
			})

			require.NoError(t, (&Client{gql: gql}).ToggleAutoMerge(context.Background(), tt.pr))
			assert.Contains(t, sent, tt.wantQuery)
			assert.Contains(t, sent, tt.wantVars)
			assert.Contains(t, sent, `"id":"`+testNodeID+`"`)
		})
	}
}

func TestToggleAutoMerge_MergesACleanPRNow(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	rest := newTestRESTClient(t, func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath = r.Method, r.URL.Path

		return jsonResponse("{}"), nil
	})
	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		t.Error("GitHub refuses auto-merge on a clean PR, so it must not be asked to")

		return jsonResponse(`{"data": {}}`), nil
	})

	pull := PR{ID: testNodeID, Repo: testRepo, Number: 7, MergeState: MergeClean}
	require.NoError(t, (&Client{gql: gql, rest: rest}).ToggleAutoMerge(context.Background(), pull))
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/repos/hugoh/gh-bulk-pr/pulls/7/merge", gotPath)
}

func TestMergesNow(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   PR
		want bool
	}{
		"clean":                 {PR{MergeState: "CLEAN"}, true},
		"clean with auto-merge": {PR{MergeState: "CLEAN", AutoMerge: true}, false},
		"behind":                {PR{MergeState: "BEHIND"}, false},
		"details not loaded":    {PR{}, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.pr.MergesNow())
		})
	}
}

func TestToggleAutoMerge_Error(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"errors": [{"message": "Pull request is in clean status"}]}`), nil
	})

	err := (&Client{gql: gql}).ToggleAutoMerge(context.Background(), PR{ID: testNodeID})
	require.ErrorContains(t, err, "clean status")
}

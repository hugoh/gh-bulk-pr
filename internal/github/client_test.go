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

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
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

const testRepo = "hugoh/gh-bulk-pr"

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

				return jsonResponse(http.StatusOK, "{}"), nil
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
      "pageInfo": {"hasNextPage": %t, "endCursor": "%s"},
      "nodes": [
        {
          "number": 1,
          "title": "Fix bug",
          "url": "https://github.com/hugoh/r/pull/1",
          "body": "body",
          "mergeStateStatus": "BEHIND",
          "repository": {"nameWithOwner": "hugoh/r"},
          "author": {"login": "hugoh"},
          "labels": {"nodes": [{"name": "bug"}]},
          "reviewRequests": {"nodes": [{"requestedReviewer": {"login": "alice"}}]},
          "commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "SUCCESS"}}}]}
        }
      ]
    }
  }
}`

func TestSearchPRs(t *testing.T) {
	t.Parallel()

	var bodies []string

	calls := 0
	gql := newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
		calls++

		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))

		hasNext := calls == 1

		cursor := ""
		if hasNext {
			cursor = "cursor1"
		}

		return jsonResponse(http.StatusOK, fmt.Sprintf(searchResponseJSON, hasNext, cursor)), nil
	})

	client := &Client{gql: gql}

	prs, err := client.SearchPRs(context.Background(), "is:open is:pr", 10)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "expected one page of pagination")
	assert.Contains(t, bodies[0], `"after":null`)
	assert.Contains(t, bodies[1], `"after":"cursor1"`)
	require.Len(t, prs, 2)

	pr := prs[0]
	assert.Equal(t, 1, pr.Number)
	assert.Equal(t, "hugoh/r", pr.Repo)
	assert.Equal(t, "hugoh", pr.Author)
	assert.Equal(t, []string{"bug"}, pr.Labels)
	assert.Equal(t, []string{"alice"}, pr.Reviewers)
	assert.Equal(t, ChecksPass, pr.Checks)
	assert.Equal(t, "BEHIND", pr.MergeState)
}

func TestSearchPRs_LimitStopsPagination(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fmt.Sprintf(searchResponseJSON, true, "cursor1")), nil
	})

	client := &Client{gql: gql}

	prs, err := client.SearchPRs(context.Background(), "is:open is:pr", 1)
	require.NoError(t, err)
	assert.Len(t, prs, 1)
}

func TestSearchPRs_TransportError(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})

	client := &Client{gql: gql}

	_, err := client.SearchPRs(context.Background(), "is:open is:pr", 10)
	require.Error(t, err)
}

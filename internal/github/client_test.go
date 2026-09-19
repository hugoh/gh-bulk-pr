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

func TestSearchPage(t *testing.T) {
	t.Parallel()

	var sent string

	gql := newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)

		return jsonResponse(fmt.Sprintf(searchResponseJSON, true, "cursor2")), nil
	})

	page, err := (&Client{gql: gql}).SearchPage(
		context.Background(),
		"is:open is:pr",
		"cursor1",
		false,
	)
	require.NoError(t, err)

	assert.Contains(t, sent, `"after":"cursor1"`)
	assert.Equal(t, 312, page.Total)
	assert.Equal(t, "cursor2", page.EndCursor)
	assert.True(t, page.HasNext)
	require.Len(t, page.PRs, 1)

	pr := page.PRs[0]
	assert.Equal(t, 1, pr.Number)
	assert.Equal(t, "hugoh/r", pr.Repo)
	assert.Equal(t, "hugoh", pr.Author)
	assert.Equal(t, []string{"bug"}, pr.Labels)
	assert.Equal(t, []string{"alice"}, pr.Reviewers)
	assert.Equal(t, ChecksPass, pr.Checks)
	assert.Equal(t, "BEHIND", pr.MergeState)
	assert.True(t, pr.Detailed)
}

func TestSearchPage_FirstPageSendsNullCursor(t *testing.T) {
	t.Parallel()

	var sent string

	gql := newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)

		return jsonResponse(fmt.Sprintf(searchResponseJSON, false, "")), nil
	})

	page, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "", false)
	require.NoError(t, err)

	assert.Contains(t, sent, `"after":null`)
	assert.False(t, page.HasNext)
}

func TestSearchPage_TransportError(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})

	_, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "", false)
	require.Error(t, err)
}

func TestSearchPage_LightSkipsExpensiveFields(t *testing.T) {
	t.Parallel()

	var sent string

	gql := newTestGraphQLClient(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)

		return jsonResponse(fmt.Sprintf(searchResponseJSON, false, "")), nil
	})

	page, err := (&Client{gql: gql}).SearchPage(context.Background(), "is:open is:pr", "", true)
	require.NoError(t, err)
	require.Len(t, page.PRs, 1)
	assert.Equal(t, "Fix bug", page.PRs[0].Title)
	assert.Equal(t, "hugoh/r", page.PRs[0].Repo)
	assert.False(t, page.PRs[0].Detailed)

	for _, field := range []string{"mergeStateStatus", "statusCheckRollup", "reviewRequests", "labels"} {
		assert.NotContains(t, sent, field)
	}
}

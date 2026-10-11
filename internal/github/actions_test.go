package github

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"merge pr with the repo's method": {
			call: func(ctx context.Context, c *Client) error {
				return c.MergePR(ctx, PR{Repo: testRepo, Number: 7, MergeMethod: MergeRebase})
			},
			wantMethod:     http.MethodPut,
			wantPath:       "/repos/hugoh/gh-bulk-pr/pulls/7/merge",
			wantBodySubstr: `"merge_method":"rebase"`,
		},
		"approve pr": {
			call: func(ctx context.Context, c *Client) error {
				return c.ApprovePR(ctx, PR{Repo: testRepo, Number: 7})
			},
			wantMethod:     http.MethodPost,
			wantPath:       "/repos/hugoh/gh-bulk-pr/pulls/7/reviews",
			wantBodySubstr: `"event":"APPROVE"`,
		},
		"update branch": {
			call: func(ctx context.Context, c *Client) error {
				return c.UpdateBranch(ctx, PR{Repo: testRepo, Number: 7})
			},
			wantMethod:     http.MethodPut,
			wantPath:       "/repos/hugoh/gh-bulk-pr/pulls/7/update-branch",
			wantBodySubstr: `{}`,
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
		"update branch": func() error {
			return client.UpdateBranch(context.Background(), PR{Repo: testRepo, Number: 1})
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, call())
		})
	}
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
			wantVars:  `"method":"SQUASH"`,
		},
		"enables with the repo's method": {
			pr:        PR{ID: testNodeID, MergeMethod: MergeCommit},
			wantQuery: "enablePullRequestAutoMerge",
			wantVars:  `"method":"MERGE"`,
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

func TestToggleAutoMerge_Error(t *testing.T) {
	t.Parallel()

	gql := newTestGraphQLClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"errors": [{"message": "Pull request is in clean status"}]}`), nil
	})

	err := (&Client{gql: gql}).ToggleAutoMerge(context.Background(), PR{ID: testNodeID})
	require.ErrorContains(t, err, "clean status")
}

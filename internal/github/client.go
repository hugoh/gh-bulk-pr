// Package github wraps the GitHub API calls gh-bulk-pr needs: searching for
// PRs and applying bulk actions to them.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
)

// PR is the subset of pull request data the list and preview views need.
type PR struct {
	ID         string // GraphQL node ID, which the auto-merge mutations need
	Number     int
	Title      string
	Repo       string // "owner/name"
	Author     string
	Labels     []string
	Reviewers  []string
	Checks     string // ChecksPass, ChecksFail, or "" when there are none
	AutoMerge  bool   // auto-merge is enabled: it merges once its requirements pass
	MergeState string // raw GraphQL mergeStateStatus, e.g. "CLEAN", "BEHIND"
	Body       string
	URL        string
	Detailed   bool // false until Details has filled in checks, merge state, labels, reviewers and body
}

// MergesNow reports whether toggling auto-merge on this PR merges it
// outright: GitHub refuses auto-merge on a PR that is already clean, so there
// is nothing to wait for.
func (p PR) MergesNow() bool {
	return !p.AutoMerge && p.MergeState == MergeClean
}

// Detail is what SearchPage leaves out because GitHub computes it per PR and
// it is slow: merge state, check rollup, labels, reviewers and body.
type Detail struct {
	ID         string
	Body       string
	MergeState string
	Checks     string
	Labels     []string
	Reviewers  []string
}

// WithDetail returns p with d's fields filled in and Detailed set.
func (p PR) WithDetail(d Detail) PR {
	p.Body, p.MergeState, p.Checks = d.Body, d.MergeState, d.Checks
	p.Labels, p.Reviewers = d.Labels, d.Reviewers
	p.Detailed = true

	return p
}

// Page is one page of search results plus what's needed to fetch the next.
type Page struct {
	PRs       []PR
	Total     int // every match GitHub reports, not just this page
	EndCursor string
	HasNext   bool
}

// MergeClean is mergeStateStatus for a PR with nothing left to wait for.
const MergeClean = "CLEAN"

// Check states reported in PR.Checks.
const (
	ChecksPass = "pass"
	ChecksFail = "fail"
)

// Client talks to the GitHub API using the token gh auth login already set up.
type Client struct {
	gql  *api.GraphQLClient
	rest *api.RESTClient
}

// NewClient builds a Client authenticated with the local gh CLI's token.
func NewClient() (*Client, error) {
	gql, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("graphql client: %w", err)
	}

	rest, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("rest client: %w", err)
	}

	return &Client{gql: gql, rest: rest}, nil
}

const (
	searchQuery = `
query($q: String!, $count: Int!, $after: String) {
  search(query: $q, type: ISSUE, first: $count, after: $after) {
    issueCount
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {
        id
        number
        title
        url
        repository { nameWithOwner }
        author { login }
        autoMergeRequest { enabledAt }
      }
    }
  }
}`

	// detailsQuery is the slow half. Cost is one point per request whatever
	// the batch size, but latency grows with it, at roughly 0.2s per PR.
	detailsQuery = `
query($ids: [ID!]!) {
  nodes(ids: $ids) {
    ... on PullRequest {
      id
      body
      mergeStateStatus
      labels(first: 20) { nodes { name } }
      reviewRequests(first: 20) { nodes { requestedReviewer {
        ... on User { login }
        ... on Team { name }
      } } }
      commits(last: 1) {
        nodes { commit { statusCheckRollup { state } } }
      }
    }
  }
}`
)

type searchResponse struct {
	Search struct {
		IssueCount int
		PageInfo   struct {
			HasNextPage bool
			EndCursor   string
		}
		Nodes []searchNode
	}
}

type searchNode struct {
	ID               string
	AutoMergeRequest *struct{ EnabledAt string }
	Number           int
	Title            string
	URL              string
	Repository       struct{ NameWithOwner string }
	Author           struct{ Login string }
}

type detailsResponse struct {
	Nodes []detailNode
}

// detailNode's ID is empty when the ID no longer resolves to a PR.
type detailNode struct {
	ID               string
	Body             string
	MergeStateStatus string
	Labels           struct {
		Nodes []struct{ Name string }
	}
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer struct {
				Login string
				Name  string
			}
		}
	}
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup struct{ State string }
			}
		}
	}
}

// SearchPageSize is how many PRs each search request fetches.
const SearchPageSize = 50

// SearchPage fetches one page of a GitHub search query (e.g.
// "is:open is:pr involves:@me"), starting after cursor ("" for the first
// page). It leaves out everything slow, so the PRs come back with Detailed
// false; Details fills them in.
func (c *Client) SearchPage(ctx context.Context, query, after string) (Page, error) {
	var cursor *string
	if after != "" {
		cursor = &after
	}

	var resp searchResponse

	vars := map[string]any{"q": query, "count": SearchPageSize, "after": cursor}
	if err := c.gql.DoWithContext(ctx, searchQuery, vars, &resp); err != nil {
		return Page{}, fmt.Errorf("search prs: %w", err)
	}

	page := Page{
		Total:     resp.Search.IssueCount,
		EndCursor: resp.Search.PageInfo.EndCursor,
		HasNext:   resp.Search.PageInfo.HasNextPage,
	}

	for _, node := range resp.Search.Nodes {
		page.PRs = append(page.PRs, prFromNode(node))
	}

	return page, nil
}

func prFromNode(node searchNode) PR {
	return PR{
		ID:        node.ID,
		AutoMerge: node.AutoMergeRequest != nil,
		Number:    node.Number,
		Title:     node.Title,
		Repo:      node.Repository.NameWithOwner,
		Author:    node.Author.Login,
		URL:       node.URL,
	}
}

// Details fetches the slow fields for the PRs with the given node IDs in one
// request. An ID that no longer resolves to a PR is left out of the result.
func (c *Client) Details(ctx context.Context, ids []string) ([]Detail, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var resp detailsResponse
	if err := c.gql.DoWithContext(
		ctx,
		detailsQuery,
		map[string]any{"ids": ids},
		&resp,
	); err != nil {
		return nil, fmt.Errorf("pr details: %w", err)
	}

	details := make([]Detail, 0, len(resp.Nodes))

	for _, node := range resp.Nodes {
		if node.ID != "" {
			details = append(details, detailFromNode(node))
		}
	}

	return details, nil
}

func detailFromNode(node detailNode) Detail {
	result := Detail{ID: node.ID, Body: node.Body, MergeState: node.MergeStateStatus}
	for _, label := range node.Labels.Nodes {
		result.Labels = append(result.Labels, label.Name)
	}

	for _, req := range node.ReviewRequests.Nodes {
		name := req.RequestedReviewer.Login
		if name == "" {
			name = req.RequestedReviewer.Name
		}

		result.Reviewers = append(result.Reviewers, name)
	}

	if len(node.Commits.Nodes) > 0 {
		switch node.Commits.Nodes[0].Commit.StatusCheckRollup.State {
		case "SUCCESS":
			result.Checks = ChecksPass
		case "FAILURE", "ERROR":
			result.Checks = ChecksFail
		}
	}

	return result
}

// AddLabel adds a label to a PR (issues and PRs share the same labels endpoint).
func (c *Client) AddLabel(ctx context.Context, pr PR, label string) error {
	return c.do(ctx, "add label", http.MethodPost,
		fmt.Sprintf("repos/%s/issues/%d/labels", pr.Repo, pr.Number),
		map[string][]string{"labels": {label}})
}

// ClosePR closes a PR without merging it.
func (c *Client) ClosePR(ctx context.Context, pr PR) error {
	return c.do(ctx, "close pr", http.MethodPatch,
		fmt.Sprintf("repos/%s/pulls/%d", pr.Repo, pr.Number),
		map[string]string{"state": "closed"})
}

// MergePR merges a PR using the squash strategy.
func (c *Client) MergePR(ctx context.Context, pr PR) error {
	return c.do(ctx, "merge pr", http.MethodPut,
		fmt.Sprintf("repos/%s/pulls/%d/merge", pr.Repo, pr.Number),
		map[string]string{"merge_method": "squash"})
}

const (
	enableAutoMergeMutation = `
mutation($id: ID!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: SQUASH}) { clientMutationId }
}`
	disableAutoMergeMutation = `
mutation($id: ID!) {
  disablePullRequestAutoMerge(input: {pullRequestId: $id}) { clientMutationId }
}`
)

// ToggleAutoMerge turns auto-merge (squash) off for a PR that has it on, and
// on for one that doesn't. A PR that is already clean has nothing to wait
// for, so it is squash-merged instead (see PR.MergesNow).
func (c *Client) ToggleAutoMerge(ctx context.Context, pull PR) error {
	if pull.MergesNow() {
		return c.MergePR(ctx, pull)
	}

	mutation := enableAutoMergeMutation
	if pull.AutoMerge {
		mutation = disableAutoMergeMutation
	}

	if err := c.gql.DoWithContext(ctx, mutation, map[string]any{"id": pull.ID}, nil); err != nil {
		return fmt.Errorf("toggle auto-merge: %w", err)
	}

	return nil
}

func (c *Client) do(ctx context.Context, what, method, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	if err := c.rest.DoWithContext(ctx, method, path, bytes.NewReader(payload), nil); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	return nil
}

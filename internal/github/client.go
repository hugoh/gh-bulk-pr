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
	Number     int
	Title      string
	Repo       string // "owner/name"
	Author     string
	Labels     []string
	Reviewers  []string
	Checks     string // ChecksPass, ChecksFail, or "" when there are none
	MergeState string // raw GraphQL mergeStateStatus, e.g. "CLEAN", "BEHIND"
	Body       string
	URL        string
	Detailed   bool // false for rows from the light search: no checks, merge state, labels or reviewers
}

// Page is one page of search results plus what's needed to fetch the next.
type Page struct {
	PRs       []PR
	Total     int // every match GitHub reports, not just this page
	EndCursor string
	HasNext   bool
}

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
	searchQueryHead = `
query($q: String!, $count: Int!, $after: String) {
  search(query: $q, type: ISSUE, first: $count, after: $after) {
    issueCount
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {`
	searchQueryTail = `
      }
    }
  }
}`

	// lightFields is enough to paint the list; GitHub answers it about twice
	// as fast as the full set because it skips merge state and check rollups.
	lightFields = `
        number
        title
        url
        repository { nameWithOwner }
        author { login }`

	fullFields = lightFields + `
        body
        mergeStateStatus
        labels(first: 20) { nodes { name } }
        reviewRequests(first: 20) { nodes { requestedReviewer {
          ... on User { login }
          ... on Team { name }
        } } }
        commits(last: 1) {
          nodes { commit { statusCheckRollup { state } } }
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
	Number           int
	Title            string
	URL              string
	Body             string
	MergeStateStatus string
	Repository       struct{ NameWithOwner string }
	Author           struct{ Login string }
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
// page). light skips labels, reviewers, body, checks and merge state, which
// GitHub answers about twice as fast; those PRs come back with Detailed false.
// Cursors are positional, so a light and a full call with the same cursor
// describe the same page.
func (c *Client) SearchPage(ctx context.Context, query, after string, light bool) (Page, error) {
	fields := fullFields
	if light {
		fields = lightFields
	}

	var cursor *string
	if after != "" {
		cursor = &after
	}

	var resp searchResponse

	vars := map[string]any{"q": query, "count": SearchPageSize, "after": cursor}
	if err := c.gql.DoWithContext(
		ctx,
		searchQueryHead+fields+searchQueryTail,
		vars,
		&resp,
	); err != nil {
		return Page{}, fmt.Errorf("search prs: %w", err)
	}

	page := Page{
		Total:     resp.Search.IssueCount,
		EndCursor: resp.Search.PageInfo.EndCursor,
		HasNext:   resp.Search.PageInfo.HasNextPage,
	}

	for _, node := range resp.Search.Nodes {
		pr := prFromNode(node)
		pr.Detailed = !light
		page.PRs = append(page.PRs, pr)
	}

	return page, nil
}

func prFromNode(node searchNode) PR {
	result := PR{
		Number:     node.Number,
		Title:      node.Title,
		Repo:       node.Repository.NameWithOwner,
		Author:     node.Author.Login,
		MergeState: node.MergeStateStatus,
		Body:       node.Body,
		URL:        node.URL,
	}
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

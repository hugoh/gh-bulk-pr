// Package github wraps the GitHub API calls gh-bulk-pr needs: searching for
// PRs and applying bulk actions to them.
package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
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
        updatedAt
        title
        url
        repository { nameWithOwner squashMergeAllowed mergeCommitAllowed rebaseMergeAllowed }
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
      state
      closedAt
      autoMergeRequest { enabledAt }
      body
      mergeStateStatus
      labels(first: 20) { nodes { name } }
      reviewRequests(first: 20) { nodes { requestedReviewer {
        ... on User { login }
        ... on Team { name }
      } } }
      commits(last: 1) {
        nodes { commit { statusCheckRollup {
          state
          contexts(first: 50) { nodes {
            ... on StatusContext { context state }
            ... on CheckRun { name status }
          } }
        } } }
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
	UpdatedAt        time.Time
	Title            string
	URL              string
	Repository       struct {
		NameWithOwner      string
		SquashMergeAllowed bool
		MergeCommitAllowed bool
		RebaseMergeAllowed bool
	}
	Author struct{ Login string }
}

// mergeMethod is the method a PR in node's repo merges with: squash when the
// repo allows it, else the first it does allow.
func (node searchNode) mergeMethod() string {
	switch repo := node.Repository; {
	case !repo.SquashMergeAllowed && repo.MergeCommitAllowed:
		return MergeCommit
	case !repo.SquashMergeAllowed && repo.RebaseMergeAllowed:
		return MergeRebase
	}

	return MergeSquash
}

type detailsResponse struct {
	Nodes []detailNode
}

// detailNode's ID is empty when the ID no longer resolves to a PR.
type detailNode struct {
	ID               string
	State            string
	ClosedAt         time.Time
	AutoMergeRequest *struct{ EnabledAt string }
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
				StatusCheckRollup struct {
					State    string
					Contexts struct {
						Nodes []checkContext
					}
				}
			}
		}
	}
}

func (n detailNode) closed() bool { return n.State == "MERGED" || n.State == "CLOSED" }

// checkContext is a commit status (Context, State) or a check run (Name,
// Status); the fields of the other kind stay empty.
type checkContext struct {
	Context string
	State   string
	Name    string
	Status  string
}

func (c checkContext) pendingName() string {
	switch {
	case c.Context != "" && (c.State == "PENDING" || c.State == "EXPECTED"):
		return c.Context
	case c.Name != "" && c.Status != "" && c.Status != "COMPLETED":
		return c.Name
	}

	return ""
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
		ID:          node.ID,
		AutoMerge:   node.AutoMergeRequest != nil,
		MergeMethod: node.mergeMethod(),
		Number:      node.Number,
		UpdatedAt:   node.UpdatedAt,
		Title:       node.Title,
		Repo:        node.Repository.NameWithOwner,
		Author:      node.Author.Login,
		URL:         node.URL,
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
	); err != nil && !onlyNotFound(err) {
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

// onlyNotFound reports whether err is GitHub saying some IDs no longer
// resolve: their nodes come back null, the rest of the response is good.
func onlyNotFound(err error) bool {
	var gqlErr *api.GraphQLError

	return errors.As(err, &gqlErr) && !slices.ContainsFunc(
		gqlErr.Errors,
		func(item api.GraphQLErrorItem) bool { return item.Type != "NOT_FOUND" },
	)
}

func detailFromNode(node detailNode) Detail {
	result := Detail{
		ID:         node.ID,
		Closed:     node.closed(),
		ClosedAt:   node.ClosedAt,
		AutoMerge:  node.AutoMergeRequest != nil,
		Body:       node.Body,
		MergeState: node.MergeStateStatus,
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
		rollup := node.Commits.Nodes[0].Commit.StatusCheckRollup

		switch rollup.State {
		case "SUCCESS":
			result.Checks = ChecksPass
		case "FAILURE", "ERROR":
			result.Checks = ChecksFail
		case "PENDING", "EXPECTED":
			result.Checks = ChecksPending
		}

		for _, ctx := range rollup.Contexts.Nodes {
			if name := ctx.pendingName(); name != "" {
				result.Pending = append(result.Pending, name)
			}
		}
	}

	return result
}

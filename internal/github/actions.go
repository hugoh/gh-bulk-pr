package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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

// MergePR merges a PR with the method its repo allows, squash if it can.
func (c *Client) MergePR(ctx context.Context, pr PR) error {
	return c.do(ctx, "merge pr", http.MethodPut,
		fmt.Sprintf("repos/%s/pulls/%d/merge", pr.Repo, pr.Number),
		map[string]string{"merge_method": pr.mergeMethod()})
}

// ApprovePR submits an approving review.
func (c *Client) ApprovePR(ctx context.Context, pr PR) error {
	return c.do(ctx, "approve pr", http.MethodPost,
		fmt.Sprintf("repos/%s/pulls/%d/reviews", pr.Repo, pr.Number),
		map[string]string{"event": "APPROVE"})
}

// UpdateBranch merges the base branch into a PR that is behind it.
func (c *Client) UpdateBranch(ctx context.Context, pr PR) error {
	return c.do(ctx, "update branch", http.MethodPut,
		fmt.Sprintf("repos/%s/pulls/%d/update-branch", pr.Repo, pr.Number),
		struct{}{})
}

const (
	enableAutoMergeMutation = `
mutation($id: ID!, $method: PullRequestMergeMethod!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId }
}`
	disableAutoMergeMutation = `
mutation($id: ID!) {
  disablePullRequestAutoMerge(input: {pullRequestId: $id}) { clientMutationId }
}`
)

// ToggleAutoMerge turns auto-merge off for a PR that has it on, and on, with
// its repo's merge method, for one that doesn't. A PR that is already clean
// has nothing to wait for, so it is merged instead (see PR.MergesNow).
func (c *Client) ToggleAutoMerge(ctx context.Context, pull PR) error {
	if pull.MergesNow() {
		return c.MergePR(ctx, pull)
	}

	mutation, vars := enableAutoMergeMutation, map[string]any{
		"id":     pull.ID,
		"method": strings.ToUpper(pull.mergeMethod()),
	}
	if pull.AutoMerge {
		mutation, vars = disableAutoMergeMutation, map[string]any{"id": pull.ID}
	}

	if err := c.gql.DoWithContext(ctx, mutation, vars, nil); err != nil {
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

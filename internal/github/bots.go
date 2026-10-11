package github

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// rebaseBox marks Renovate's rebase/retry checkbox, which its PR bodies end
// with; a self-hosted Renovate is known by it alone. Renovate removes
// rebaseLabel, or unticks the box, once it has rebased.
const (
	rebaseBox   = "<!-- rebase-check -->"
	rebaseBoxOn = "[x] " + rebaseBox
	rebaseLabel = "rebase"
)

const (
	renovateLogin   = "renovate"
	dependabotLogin = "dependabot"
)

// Renovate reports whether Renovate manages the PR, so a rebase is asked of
// it rather than done on GitHub (see RequestRebase): the hosted app by its
// login, a self-hosted one by its checkbox.
func (p PR) Renovate() bool {
	return p.authorLogin() == renovateLogin || strings.Contains(p.Body, rebaseBox)
}

func (p PR) authorLogin() string { return strings.TrimSuffix(p.Author, "[bot]") }

// Bot names the dependency bot that manages the PR, "Renovate" or
// "Dependabot", or is "". Such a PR is rebased by asking its bot (see
// RequestRebase): the bot stops updating a branch anyone else pushes to.
func (p PR) Bot() string {
	switch {
	case p.Renovate():
		return "Renovate"
	case p.authorLogin() == dependabotLogin:
		return "Dependabot"
	}

	return ""
}

// RebaseRequested reports whether Renovate has been asked to rebase the PR
// and hasn't yet.
func (p PR) RebaseRequested() bool {
	return strings.Contains(p.Body, rebaseBoxOn) || slices.Contains(p.Labels, rebaseLabel)
}

// RequestRebase asks the PR's bot to rebase it: Dependabot in a comment,
// Renovate with its rebase label, one call that, unlike ticking the box, can't
// race Renovate rewriting the body.
func (c *Client) RequestRebase(ctx context.Context, pull PR) error {
	if pull.Bot() == "Dependabot" {
		return c.do(ctx, "request rebase", http.MethodPost,
			fmt.Sprintf("repos/%s/issues/%d/comments", pull.Repo, pull.Number),
			map[string]string{"body": "@dependabot rebase"})
	}

	return c.AddLabel(ctx, pull, rebaseLabel)
}

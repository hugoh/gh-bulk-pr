package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Renovate's PR bodies end with a checkbox that asks it to rebase or retry
// the PR when ticked; it unticks it once done.
const (
	rebaseBox    = "<!-- rebase-check -->"
	rebaseBoxOff = "[ ] " + rebaseBox
	rebaseBoxOn  = "[x] " + rebaseBox
)

// Renovate reports whether Renovate manages the PR, so a rebase is asked of
// it rather than done on GitHub (see RequestRebase).
func (p PR) Renovate() bool { return strings.Contains(p.Body, rebaseBox) }

const dependabotLogin = "dependabot"

// Bot names the dependency bot that manages the PR, "Renovate" or
// "Dependabot", or is "". Such a PR is rebased by asking its bot (see
// RequestRebase): the bot stops updating a branch anyone else pushes to.
func (p PR) Bot() string {
	switch {
	case p.Renovate():
		return "Renovate"
	case strings.TrimSuffix(p.Author, "[bot]") == dependabotLogin:
		return "Dependabot"
	}

	return ""
}

// RebaseRequested reports whether Renovate has been asked to rebase the PR
// and hasn't yet.
func (p PR) RebaseRequested() bool { return strings.Contains(p.Body, rebaseBoxOn) }

// RequestRebase asks the PR's bot to rebase it: Dependabot in a comment,
// Renovate by ticking its rebase/retry checkbox. Renovate rewrites the body,
// so it is read just before writing it back.
func (c *Client) RequestRebase(ctx context.Context, pull PR) error {
	if pull.Bot() == "Dependabot" {
		return c.do(ctx, "request rebase", http.MethodPost,
			fmt.Sprintf("repos/%s/issues/%d/comments", pull.Repo, pull.Number),
			map[string]string{"body": "@dependabot rebase"})
	}

	path := fmt.Sprintf("repos/%s/pulls/%d", pull.Repo, pull.Number)

	var current struct{ Body string }
	if err := c.rest.DoWithContext(ctx, http.MethodGet, path, nil, &current); err != nil {
		return fmt.Errorf("request rebase: %w", err)
	}

	switch {
	case strings.Contains(current.Body, rebaseBoxOn):
		return nil
	case !strings.Contains(current.Body, rebaseBoxOff):
		return errNotRenovate
	}

	return c.do(ctx, "request rebase", http.MethodPatch, path,
		map[string]string{"body": strings.Replace(current.Body, rebaseBoxOff, rebaseBoxOn, 1)})
}

var errNotRenovate = errors.New("request rebase: no Renovate rebase checkbox in the PR body")

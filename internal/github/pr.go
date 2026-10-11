package github

import (
	"cmp"
	"time"
)

// PR is the subset of pull request data the list and preview views need.
type PR struct {
	ID          string // GraphQL node ID, which the auto-merge mutations need
	Number      int
	UpdatedAt   time.Time
	Title       string
	Repo        string // "owner/name"
	Author      string
	Labels      []string
	Reviewers   []string
	Checks      string   // ChecksPass, ChecksFail, ChecksPending, or "" when there are none
	Pending     []string // names of the checks still running or waiting, e.g. "renovate/stability-days"
	AutoMerge   bool     // auto-merge is enabled: it merges once its requirements pass
	MergeMethod string   // MergeSquash, MergeCommit or MergeRebase: how its repo lets it merge; "" means squash
	Closed      bool     // merged or closed
	MergeState  string   // raw GraphQL mergeStateStatus, e.g. "CLEAN", "BEHIND"
	Body        string
	URL         string
	Detailed    bool // false until Details has filled in checks, merge state, labels, reviewers and body
}

// MergesNow reports whether toggling auto-merge on this PR merges it
// outright: GitHub refuses auto-merge on a PR that is already clean, so there
// is nothing to wait for.
func (p PR) MergesNow() bool {
	return !p.AutoMerge && p.MergeState == MergeClean
}

// Waiting reports whether the PR's state can still change without anyone
// acting on it: its checks are pending, GitHub hasn't worked out its merge
// state yet, auto-merge will merge it, or Renovate has a rebase to do. A
// closed PR waits on nothing.
func (p PR) Waiting() bool {
	return p.Detailed && !p.Closed &&
		(p.Checks == ChecksPending || p.MergeState == MergeUnknown || p.AutoMerge || p.RebaseRequested())
}

// Detail is what SearchPage leaves out because GitHub computes it per PR and
// it is slow: merge state, check rollup, labels, reviewers and body.
type Detail struct {
	ID         string
	Closed     bool // merged or closed; GitHub reports such a PR's merge state as UNKNOWN for good
	ClosedAt   time.Time
	AutoMerge  bool
	Body       string
	MergeState string
	Checks     string
	Pending    []string
	Labels     []string
	Reviewers  []string
}

// WithDetail returns p with d's fields filled in and Detailed set. d's
// AutoMerge wins over the search's: the search index lags behind.
func (p PR) WithDetail(d Detail) PR {
	p.AutoMerge, p.Closed, p.Body, p.MergeState, p.Checks = d.AutoMerge, d.Closed, d.Body, d.MergeState, d.Checks
	p.Pending, p.Labels, p.Reviewers = d.Pending, d.Labels, d.Reviewers
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

// MergeUnknown is mergeStateStatus while GitHub is still computing it.
const MergeUnknown = "UNKNOWN"

// Check states reported in PR.Checks.
const (
	ChecksPass = "pass"
	ChecksFail = "fail"
	// ChecksPending is a rollup still waiting on something: a running job or a
	// status like renovate/stability-days.
	ChecksPending = "pending"
)

// Merge methods, as the REST merge endpoint names them.
const (
	MergeSquash = "squash"
	MergeCommit = "merge"
	MergeRebase = "rebase"
)

func (p PR) mergeMethod() string { return cmp.Or(p.MergeMethod, MergeSquash) }

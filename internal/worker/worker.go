// Package worker runs a bulk action against many pull requests concurrently,
// bounded so a batch can't blow through the user's GitHub rate limit.
package worker

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/hugoh/gh-bulk-pr/internal/github"
)

const maxConcurrency = 5

// Result is one PR's outcome from a bulk action run.
type Result struct {
	PR  github.PR
	Err error
}

// Run applies action to every PR concurrently, bounded to maxConcurrency at
// a time, and returns one Result per PR in the same order as prs. done is
// incremented after each PR finishes, so a caller can poll it for progress.
func Run(
	ctx context.Context,
	prs []github.PR,
	action func(context.Context, github.PR) error,
	done *atomic.Int32,
) []Result {
	results := make([]Result, len(prs))
	slots := make(chan struct{}, maxConcurrency)

	var group sync.WaitGroup

	for idx, pull := range prs {
		slots <- struct{}{}

		group.Go(func() {
			defer func() { <-slots }()

			results[idx] = Result{PR: pull, Err: action(ctx, pull)}

			done.Add(1)
		})
	}

	group.Wait()

	return results
}

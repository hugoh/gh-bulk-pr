package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()

	failErr := errors.New("boom")

	prs := []github.PR{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	}

	var done atomic.Int32

	results := Run(context.Background(), prs, func(_ context.Context, pr github.PR) error {
		if pr.Number == 2 {
			return failErr
		}

		return nil
	}, &done)

	require.Len(t, results, 3)
	assert.Equal(t, int32(3), done.Load(), "done should count every PR, success or failure")

	for i, pr := range prs {
		assert.Equal(t, pr, results[i].PR, "results preserve input order")
	}

	require.NoError(t, results[0].Err)
	require.ErrorIs(t, results[1].Err, failErr)
	require.NoError(t, results[2].Err)
}

func TestRun_Empty(t *testing.T) {
	t.Parallel()

	var done atomic.Int32

	results := Run(context.Background(), nil, func(context.Context, github.PR) error {
		t.Fatal("action should not be called for an empty PR list")

		return nil
	}, &done)

	assert.Empty(t, results)
	assert.Equal(t, int32(0), done.Load())
}

func TestRun_BoundsConcurrency(t *testing.T) {
	t.Parallel()

	var (
		mu            sync.Mutex
		running, peak int
		done          atomic.Int32
	)

	prs := make([]github.PR, 4*maxConcurrency)
	Run(context.Background(), prs, func(context.Context, github.PR) error {
		mu.Lock()

		running++
		peak = max(peak, running)

		mu.Unlock()

		time.Sleep(5 * time.Millisecond)

		mu.Lock()

		running--

		mu.Unlock()

		return nil
	}, &done)

	assert.LessOrEqual(t, peak, maxConcurrency)
	assert.Greater(t, peak, 1, "actions should run concurrently")
}

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPR_WithDetail(t *testing.T) {
	t.Parallel()

	light := PR{ID: "PR_1", Number: 3, Title: "Fix", AutoMerge: true}
	full := light.WithDetail(Detail{
		ID: "PR_1", Body: "b", MergeState: MergeClean, Checks: ChecksPass,
		Labels: []string{"x"}, Reviewers: []string{"y"},
	})

	assert.True(t, full.Detailed)
	assert.Equal(t, "Fix", full.Title, "the search's fields are kept")
	assert.False(t, full.AutoMerge, "the details are fresher than the search index")
	assert.Equal(t, "b", full.Body)
	assert.Equal(t, MergeClean, full.MergeState)
	assert.Equal(t, ChecksPass, full.Checks)
	assert.Equal(t, []string{"x"}, full.Labels)
	assert.Equal(t, []string{"y"}, full.Reviewers)
	assert.False(t, light.Detailed, "the original is untouched")
}

func TestMergesNow(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   PR
		want bool
	}{
		"clean":                 {PR{MergeState: "CLEAN"}, true},
		"clean with auto-merge": {PR{MergeState: "CLEAN", AutoMerge: true}, false},
		"unstable":              {PR{MergeState: "UNSTABLE"}, true},
		"has hooks":             {PR{MergeState: "HAS_HOOKS"}, true},
		"behind":                {PR{MergeState: "BEHIND"}, false},
		"blocked":               {PR{MergeState: "BLOCKED"}, false},
		"details not loaded":    {PR{}, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.pr.MergesNow())
		})
	}
}

func TestPR_WithDetail_CopiesPending(t *testing.T) {
	t.Parallel()

	full := PR{}.WithDetail(Detail{Checks: ChecksPending, Pending: []string{stabilityDays}})

	assert.Equal(t, []string{stabilityDays}, full.Pending)
}

func TestPR_Waiting(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   PR
		want bool
	}{
		"pending checks": {PR{Detailed: true, Checks: ChecksPending}, true},
		"merge state unknown": {
			PR{Detailed: true, Checks: ChecksPass, MergeState: "UNKNOWN"},
			true,
		},
		"passing": {
			PR{Detailed: true, Checks: ChecksPass, MergeState: MergeClean},
			false,
		},
		"failing":            {PR{Detailed: true, Checks: ChecksFail}, false},
		"auto-merge on":      {PR{Detailed: true, Checks: ChecksPass, AutoMerge: true}, true},
		"closed":             {PR{Detailed: true, Closed: true, MergeState: MergeUnknown}, false},
		"details not loaded": {PR{Checks: ChecksPending}, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.pr.Waiting())
		})
	}
}

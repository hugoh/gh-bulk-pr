package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const plainBody = "Plain PR."

const renovateBody = "Update foo.\n\n - [ ] <!-- rebase-check -->If you want to rebase/retry this PR, check this box\n"

// prBody is the JSON a PR's body is read and written as.
type prBody struct {
	Body string `json:"body"`
}

func TestRequestRebase(t *testing.T) {
	t.Parallel()

	ticked := strings.Replace(renovateBody, "[ ]", "[x]", 1)

	tests := map[string]struct {
		current string
		want    string // the body written back, "" when nothing is
		wantErr bool
	}{
		"ticks the box":     {current: renovateBody, want: ticked},
		"already requested": {current: ticked},
		"not a renovate pr": {current: plainBody, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var written prBody

			rest := newTestRESTClient(t, func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "/repos/hugoh/gh-bulk-pr/pulls/7", r.URL.Path)

				if r.Method == http.MethodPatch {
					if err := json.NewDecoder(r.Body).Decode(&written); err != nil {
						return nil, fmt.Errorf("decode patch: %w", err)
					}

					return jsonResponse("{}"), nil
				}

				current, err := json.Marshal(prBody{Body: tt.current})
				if err != nil {
					return nil, fmt.Errorf("encode pr: %w", err)
				}

				return jsonResponse(string(current)), nil
			})

			err := (&Client{rest: rest}).RequestRebase(
				context.Background(),
				PR{Repo: testRepo, Number: 7},
			)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, written.Body)
		})
	}
}

func TestPR_Renovate(t *testing.T) {
	t.Parallel()

	ticked := strings.Replace(renovateBody, "[ ]", "[x]", 1)

	assert.True(t, PR{Body: renovateBody}.Renovate())
	assert.False(t, PR{Body: renovateBody}.RebaseRequested())
	assert.True(t, PR{Body: ticked}.RebaseRequested())
	assert.False(t, PR{Body: plainBody}.Renovate())
	assert.True(
		t,
		PR{Detailed: true, Checks: ChecksPass, Body: ticked}.Waiting(),
		"it waits on Renovate",
	)
}

func TestRequestRebase_AsksDependabotInAComment(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	var written prBody

	rest := newTestRESTClient(t, func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&written); err != nil {
			return nil, fmt.Errorf("decode comment: %w", err)
		}

		return jsonResponse("{}"), nil
	})

	pr := PR{Repo: testRepo, Number: 7, Author: dependabotLogin}
	require.NoError(t, (&Client{rest: rest}).RequestRebase(context.Background(), pr))

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/repos/hugoh/gh-bulk-pr/issues/7/comments", gotPath)
	assert.Equal(t, "@dependabot rebase", written.Body)
}

func TestPR_Bot(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pr   PR
		want string
	}{
		"renovate":          {PR{Author: "renovate", Body: renovateBody}, "Renovate"},
		"self-hosted bot":   {PR{Author: "my-renovate", Body: renovateBody}, "Renovate"},
		"dependabot":        {PR{Author: dependabotLogin}, "Dependabot"},
		"dependabot (REST)": {PR{Author: "dependabot[bot]"}, "Dependabot"},
		"person":            {PR{Author: "hugoh", Body: plainBody}, ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.pr.Bot())
		})
	}
}

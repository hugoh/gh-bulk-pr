package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColumnsForWidth(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		width         int
		wantTitleMin  bool // width too small: Title clamps to colTitleMin
		wantTitleWide int  // expected Title width when not clamped (0 = skip check)
	}{
		"very narrow clamps to minimum": {width: 10, wantTitleMin: true},
		"exactly fixed overhead":        {width: fixedColsSum + tableOverhead, wantTitleMin: true},
		"generous width": {
			width:         200,
			wantTitleWide: 200 - fixedColsSum - tableOverhead,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cols := columnsForWidth(tt.width)
			require.Len(t, cols, 7)
			assert.Equal(t, "Merge", cols[5].Title)

			titleCol := cols[3]
			assert.Equal(t, "Title", titleCol.Title)

			if tt.wantTitleMin {
				assert.Equal(t, colTitleMin, titleCol.Width)
			}

			if tt.wantTitleWide != 0 {
				assert.Equal(t, tt.wantTitleWide, titleCol.Width)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	m := New(nil, "is:open is:pr")

	assert.Equal(t, "is:open is:pr", m.query)
	assert.True(t, m.loading)
	assert.Empty(t, m.selected)
	assert.Equal(t, screenList, m.screen)
}

func TestInit(t *testing.T) {
	t.Parallel()

	m := New(&github.Client{}, "is:open is:pr")
	cmd := m.Init()
	require.NotNil(t, cmd)

	msg := cmd()
	_, ok := msg.(tea.BatchMsg)
	assert.True(t, ok, "Init should batch the search and spinner tick commands")
}

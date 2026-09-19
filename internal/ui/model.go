// Package ui implements the bubbletea TUI: a filterable, multi-selectable
// PR table with a preview pane and keyboard-driven bulk actions.
package ui

import (
	"context"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

const maxBatchSize = 50

type screen int

const (
	screenList screen = iota
	screenFilter
	screenActionInput
	screenConfirm
	screenResults
)

type pendingAction struct {
	label string // human-readable name for the confirm/results screens
	run   func(ctx context.Context, pr github.PR) error
}

// Model is the bubbletea model driving the PR list, preview panel, filter
// bar, and bulk-action flow.
type Model struct {
	client *github.Client
	query  string

	table       table.Model
	filterInput textinput.Model
	actionInput textinput.Model
	prs         []github.PR
	selected    map[prKey]bool

	screen    screen
	actionKey string // l, c, m; set while the action awaits its text input
	action    *pendingAction
	confirm   []github.PR
	results   []worker.Result

	spinner     spinner.Model
	progress    progress.Model
	actionDone  *atomic.Int32
	actionTotal int

	previewOpen bool
	err         error
	loading     bool

	width, height int
}

const (
	colSelect   = 1
	colRepo     = 22
	colNumber   = 6
	colTitleMin = 20
	colChecks   = 8
	colMerge    = 8
	colAuthor   = 12
	numCols     = 7
	cellPadding = 2 // bubbles/table's default Cell style: Padding(0, 1), left+right

	fixedColsSum       = colSelect + colRepo + colNumber + colChecks + colMerge + colAuthor
	tableOverhead      = numCols * cellPadding
	defaultTableHeight = 20
)

// columnsForWidth sizes the Title column to fill the rest of width, so the
// table always spans the full terminal width instead of a fixed 89 columns.
// Each cell carries cellPadding of its own (bubbles/table's default style),
// which counts against the available width but isn't part of any col.Width.
func columnsForWidth(width int) []table.Column {
	titleWidth := max(width-fixedColsSum-tableOverhead, colTitleMin)

	return []table.Column{
		{Title: " ", Width: colSelect},
		{Title: "Repo", Width: colRepo},
		{Title: "#", Width: colNumber},
		{Title: "Title", Width: titleWidth},
		{Title: "Checks", Width: colChecks},
		{Title: "Merge", Width: colMerge},
		{Title: "Author", Width: colAuthor},
	}
}

// New builds the initial Model, ready to fetch PRs matching query.
func New(client *github.Client, query string) Model {
	cols := columnsForWidth(0)
	keyMap := table.DefaultKeyMap()
	keyMap.LineUp = key.NewBinding(key.WithKeys("up", "k"))
	keyMap.LineDown = key.NewBinding(key.WithKeys("down", "j"))

	tableModel := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithKeyMap(keyMap),
		table.WithHeight(defaultTableHeight),
	)

	filterTI := textinput.New()
	filterTI.Placeholder = query
	filterTI.SetValue(query)

	actionTI := textinput.New()

	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	prog := progress.New(progress.WithDefaultGradient())

	return Model{
		client:      client,
		query:       query,
		table:       tableModel,
		filterInput: filterTI,
		actionInput: actionTI,
		selected:    map[prKey]bool{},
		loading:     true,
		spinner:     spin,
		progress:    prog,
	}
}

// Init kicks off the first PR search and starts the loading spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.search(m.query), m.spinner.Tick)
}

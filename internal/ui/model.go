// Package ui implements the bubbletea TUI: a filterable, multi-selectable
// PR table with a preview pane and keyboard-driven bulk actions.
package ui

import (
	"context"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/hugoh/gh-bulk-pr/internal/github"
	"github.com/hugoh/gh-bulk-pr/internal/history"
	"github.com/hugoh/gh-bulk-pr/internal/worker"
)

// loadMoreMargin is how close to the last loaded row the cursor gets before
// the next page is fetched.
const loadMoreMargin = 10

type screen int

const (
	screenList screen = iota
	screenFilter
	screenActionInput
	screenConfirm
	screenResults
	screenHelp
)

type pendingAction struct {
	label       string // human-readable name for the confirm/results screens
	destructive bool   // hard to undo: only an explicit "y" confirms it, never enter
	run         func(ctx context.Context, pr github.PR) error
	onDone      func(pr *github.PR)       // optional: brings a succeeded PR's local copy up to date
	note        func(pr github.PR) string // optional: what this action does to pr, when it isn't obvious
}

// Model is the bubbletea model driving the PR list, preview panel, filter
// bar, and bulk-action flow.
type Model struct {
	client  *github.Client
	keys    keyMap
	help    help.Model
	query   string
	tab     int // index into tabs, or noTab when query matches none
	history *history.Log

	filterHistory []string // past queries, oldest first, loaded when the filter bar opens
	filterPos     int      // index into filterHistory; len means the draft being typed
	filterDraft   string

	table       table.Model
	filterInput textinput.Model
	actionInput textinput.Model
	prs         []github.PR
	cache       map[string]github.Page // last full result per query
	total       int                    // every match GitHub reports, loaded or not
	endCursor   string                 // where the next page starts
	hasMore     bool
	selected    map[prKey]bool

	screen    screen
	actionKey string // l, c, m; set while the action awaits its text input
	action    *pendingAction
	confirm   []github.PR
	results   []worker.Result
	pane      viewport.Model // scrolls the confirm and results lists

	spinner     spinner.Model
	progress    progress.Model
	actionDone  *atomic.Int32
	actionTotal int

	searchID int // identifies the latest search; older results are dropped
	cancel   context.CancelFunc

	details   map[string]github.Detail // last known details by PR ID; kept across refreshes
	fetched   map[string]fetchState    // per PR ID, for the current search only
	fetching  int                      // details requests in flight
	detailCtx context.Context          //nolint:containedctx // ends with the search, like cancel
	detailErr error                    // last failure loading details; the rows stay usable

	cancelMore context.CancelFunc // stops the further page being fetched, if any

	open        func(url string) error // opens a URL in the browser; replaced in tests
	notice      string                 // a one-off message for the status line, cleared by the next key
	openArmed   bool                   // O was pressed with a large selection; a second O opens them
	quitArmed   bool                   // q was pressed with a selection; a second q quits
	previewOpen bool
	mouseMode   tea.MouseMode // set once via WithMouse; reported by View
	err         error
	moreErr     error // last failure loading a further page; the list stays usable
	loading     bool  // first page of a search in flight
	loadingMore bool  // a further page in flight

	width, height int
}

const (
	colSelect   = 1
	colRepo     = 22
	colNumber   = 6
	colTitleMin = 20
	colChecks   = 8
	colMerge    = 8
	colAuto     = 4
	colAuthor   = 12
	numCols     = 8
	cellPadding = 2 // bubbles/table's default Cell style: Padding(0, 1), left+right

	fixedColsSum       = colSelect + colRepo + colNumber + colChecks + colMerge + colAuto + colAuthor
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
		{Title: "Auto", Width: colAuto},
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
		table.WithStyles(tableStyles()),
	)

	filterTI := textinput.New()
	filterTI.Placeholder = query
	filterTI.SetValue(query)

	actionTI := textinput.New()

	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(fg(colorAccent())))
	prog := progress.New(progress.WithColors(colorAccent(), colorInfo()))

	return Model{
		client:      client,
		keys:        newKeyMap(),
		open:        browse,
		help:        newHelp(),
		query:       query,
		tab:         tabFor(query),
		table:       tableModel,
		filterInput: filterTI,
		actionInput: actionTI,
		pane:        viewport.New(),
		selected:    map[prKey]bool{},
		cache:       map[string]github.Page{},
		details:     map[string]github.Detail{},
		fetched:     map[string]fetchState{},
		detailCtx:   context.Background(),
		loading:     true,
		spinner:     spin,
		progress:    prog,
	}
}

// WithHistory records every query run from the filter bar or a tab into h.
func (m Model) WithHistory(h *history.Log) Model {
	m.history = h

	return m
}

// WithMouse enables mouse wheel scrolling: the terminal then needs shift to
// select text. Mouse mode is a per-render View field in bubbletea v2, so it's
// stored on the model instead of a tea.ProgramOption.
func (m Model) WithMouse(enabled bool) Model {
	if enabled {
		m.mouseMode = tea.MouseModeCellMotion
	} else {
		m.mouseMode = tea.MouseModeNone
	}

	return m
}

// Init kicks off the first PR search and starts the loading spinner.
func (m Model) Init() tea.Cmd {
	return m.searchCmds(context.Background())
}

func newHelp() help.Model {
	h := help.New()
	h.Styles = helpStyles()
	h.ShortSeparator = " · "

	return h
}

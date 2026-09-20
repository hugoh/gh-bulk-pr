package ui

import "charm.land/bubbles/v2/key"

// keyMap is every key the list screen responds to, with its help text: the
// footer and the "?" screen are generated from it. Up/Down/Top/Bottom/Page*
// are handled by the table itself and appear here only so the help lists them.
type keyMap struct {
	Move, Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	Select, SelectAll, Clear, Preview             key.Binding
	Filter, Tab, Refresh, Checks, Open, OpenAll   key.Binding
	Label, Close, Merge, AutoMerge                key.Binding
	Help, Quit                                    key.Binding
}

func binding(helpKey, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(helpKey, desc))
}

func newKeyMap() keyMap {
	return keyMap{
		Move:      binding("j/k", "move", "up", "down", "j", "k"),
		Up:        binding("↑/k", "up", "up", "k"),
		Down:      binding("↓/j", "down", "down", "j"),
		Top:       binding("g/home", "top", "g", "home"),
		Bottom:    binding("G/end", "bottom", "G", "end"),
		PageUp:    binding("pgup/b", "page up", "pgup", "b"),
		PageDown:  binding("pgdn/f", "page down", "pgdown", "f"),
		Select:    binding("x/space", "select", "x", "space"),
		SelectAll: binding("ctrl+a", "select all", "ctrl+a"),
		Clear:     binding("esc", "close/clear", keyEsc),
		Preview:   binding("enter/p", "preview", keyEnter, "p"),
		Filter:    binding("/", "filter", "/"),
		Tab:       binding("1/2", "tab", "1", "2"),
		Refresh:   binding("r", "refresh", "r"),
		Checks:    binding("T", "checks", "T"),
		Open:      binding("o", "open", "o"),
		OpenAll:   binding("O", "open all", "O"),
		Label:     binding("l", "label", "l"),
		Close:     binding("c", "close", "c"),
		Merge:     binding("m", "merge", "m"),
		AutoMerge: binding("a", "auto", "a"),
		Help:      binding("?", "help", "?"),
		Quit:      binding("q", "quit", "q"),
	}
}

// short is the footer when nothing is selected.
func (k keyMap) short() []key.Binding {
	return []key.Binding{k.Move, k.Select, k.Preview, k.Open, k.Filter, k.Refresh, k.Help, k.Quit}
}

// shortSelected is the footer once PRs are selected: the actions come first.
func (k keyMap) shortSelected() []key.Binding {
	return []key.Binding{k.Label, k.Close, k.Merge, k.AutoMerge, k.OpenAll, k.Clear, k.Help}
}

// full is the "?" screen, one column per group.
func (k keyMap) full() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.PageUp, k.PageDown},
		{k.Select, k.SelectAll, k.Clear, k.Preview},
		{k.Filter, k.Tab, k.Refresh, k.Checks, k.Open, k.OpenAll},
		{k.Label, k.Close, k.Merge, k.AutoMerge, k.Help, k.Quit},
	}
}

package ui

import "charm.land/bubbles/v2/key"

// Values the README states, exported so its generator reads them from the code.
const (
	BaseQuery           = baseQuery
	OpenAllConfirmAbove = openAllConfirmAbove
	OpenAllMax          = openAllMax
	MinWidth            = minWidth
	MinHeight           = minHeight
)

// HelpKeys is the "?" screen's bindings, flattened in display order.
func HelpKeys() []key.Binding {
	var all []key.Binding
	for _, group := range newKeyMap().full() {
		all = append(all, group...)
	}

	return all
}

// TabNames lists the tabs' query suffixes in order, as the 1/2/... keys select them.
func TabNames() []string {
	names := make([]string, 0, len(tabs()))
	for _, t := range tabs() {
		names = append(names, t.name)
	}

	return names
}

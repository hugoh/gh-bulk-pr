package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

const day = 24 * time.Hour

// clockTickMsg re-renders the view so relative ages keep moving.
type clockTickMsg struct{}

func clockTick() tea.Cmd {
	return tea.Tick(time.Minute, func(time.Time) tea.Msg { return clockTickMsg{} })
}

// age is the compact column form: "5m", "3h", "2d".
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	elapsed := time.Since(t)

	switch {
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < day:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed/day))
	}
}

// ago is the footer form: "just now", "1 min ago", "5 mins ago", "2 hours ago".
func ago(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < 2*time.Minute:
		return "1 min ago"
	case elapsed < time.Hour:
		return fmt.Sprintf("%d mins ago", int(elapsed.Minutes()))
	case elapsed < 2*time.Hour:
		return "1 hour ago"
	default:
		return fmt.Sprintf("%d hours ago", int(elapsed.Hours()))
	}
}

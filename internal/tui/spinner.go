package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Braille spinner frames (same as ccx). 80ms tick, gated on loading so idle
// is 0fps — never redraw on a fixed timer when nothing changes.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// renderSpinner returns a colored spinner + label. Single helper so we never
// triplicate the color-cycling render like ccx did.
func renderSpinner(frame int, label string) string {
	idx := frame % len(spinnerFrames)
	color := spinnerColors[(frame/len(spinnerColors))%len(spinnerColors)]
	s := lipgloss.NewStyle().Foreground(color).Bold(true)
	return "  " + s.Render(spinnerFrames[idx]+" "+label)
}

// prListPollCmd arms the next background poll at the configured interval.
func prListPollCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return prListTickMsg(t) })
}

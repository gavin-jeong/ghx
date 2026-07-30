package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// initListBase applies standard styling: hide title/status/filter/pagination/help,
// disable quit keybindings. Mirrors ccx selection.go.
func initListBase(l *list.Model) {
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
}

// configureListSearch sets the filter prompt and arrow-only cursor.
func configureListSearch(l *list.Model) {
	l.FilterInput.Prompt = "Search: "
	l.KeyMap.AcceptWhileFiltering = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "apply"),
	)
	l.KeyMap.CursorUp = key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "up"),
	)
	l.KeyMap.CursorDown = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "down"),
	)
}

// setListItemsPreservingFilter replaces items and re-runs any active filter.
// bubbles' SetItems clears filteredItems and returns the re-filter as a cmd
// callers drop — re-applying SetFilterText synchronously keeps the visible
// set correct and restores the cursor.
func setListItemsPreservingFilter(l *list.Model, items []list.Item) {
	state := l.FilterState()
	filter := l.FilterInput.Value()
	if state == list.Unfiltered || filter == "" {
		l.SetItems(items)
		return
	}
	savedIndex := l.Index()
	l.SetItems(items)
	l.SetFilterText(filter)
	if state == list.Filtering {
		l.SetFilterState(list.Filtering)
	}
	if n := len(l.VisibleItems()); n > 0 {
		if savedIndex < 0 {
			savedIndex = 0
		}
		if savedIndex >= n {
			savedIndex = n - 1
		}
		l.Select(savedIndex)
	}
}

// listFilterTerm returns the active filter term, or "" if not filtering.
func listFilterTerm(m list.Model) string {
	if m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied {
		return m.FilterValue()
	}
	return ""
}

// startListSearch activates the filter input. Placeholder — Phase 6 fills in.
func startListSearch(l *list.Model) tea.Cmd {
	if l.Width() == 0 {
		return nil
	}
	if v := l.FilterInput.Value(); v != "" {
		l.SetFilterText(v)
	}
	openMsg := tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'/'}})
	newL, cmd := l.Update(openMsg)
	*l = newL
	return cmd
}

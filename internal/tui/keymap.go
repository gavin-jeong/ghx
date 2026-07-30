package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Keymap holds the key bindings for ghx. Fields are strings (the tea.KeyMsg
// String() form) so YAML overrides and TranslateNav can map aliases.
type Keymap struct {
	// Navigation (vim aliases resolved by TranslateNav)
	Up    []string
	Down  []string
	Left  []string
	Right []string

	// PR list
	Open      string // enter
	Search    string // /
	Refresh   string // R
	NextTab   string // tab
	PrevTab   string // shift+tab
	Quit      string // q

	// PR detail
	Back           string // esc
	Approve        string // a
	RequestChanges string // r
	Comment        string // c
	IssueComment   string // C
	OpenBrowser    string // o
	Checkout       string // co (two-key chord handled in detail)
	Ready          string // rd (two-key chord)
	Merge          string // M
	NextDetailTab  string // l
	PrevDetailTab  string // h

	// Global
	Palette string // :
	Help    string // ?
}

// DefaultKeymap returns the built-in bindings.
func DefaultKeymap() *Keymap {
	return &Keymap{
		Up: []string{"k", "up"}, Down: []string{"j", "down"},
		Left: []string{"h", "left"}, Right: []string{"l", "right"},
		Open: "enter", Search: "/", Refresh: "R",
		NextTab: "tab", PrevTab: "shift+tab", Quit: "q",
		Back: "esc", Approve: "a", RequestChanges: "r",
		Comment: "c", IssueComment: "C", OpenBrowser: "o",
		Checkout: "co", Ready: "rd", Merge: "M",
		NextDetailTab: "l", PrevDetailTab: "h",
		Palette: ":", Help: "?",
	}
}

// matchKey reports whether key equals any of the given bindings.
func matchKey(key string, bindings ...string) bool {
	for _, b := range bindings {
		if key == b {
			return true
		}
	}
	return false
}

// isNavKey reports whether key is a navigation alias (so the list should NOT
// enter filter mode on it — mirrors ccx's isNavKey).
func isNavKey(key string) bool {
	switch key {
	case "k", "j", "h", "l", "g", "G", "up", "down", "left", "right",
		"ctrl+b", "ctrl+f", "ctrl+d", "ctrl+u", "pgup", "pgdown", "home", "end":
		return true
	}
	return false
}

// translateNav normalizes navigation aliases into canonical tea.KeyMsg forms
// so the rest of the code handles one vocabulary. Returns the original key and
// false if it's not a nav alias.
func translateNav(msg tea.KeyMsg) (string, bool) {
	s := msg.String()
	switch s {
	case "k":
		return "up", true
	case "j":
		return "down", true
	case "h":
		return "left", true
	case "l":
		return "right", true
	case "G":
		return "end", true
	case "g":
		return "home", true
	case "ctrl+b":
		return "pgup", true
	case "ctrl+f":
		return "pgdown", true
	}
	return s, false
}

// displayKey maps internal key names to footer glyphs.
func displayKey(key string) string {
	switch key {
	case " ":
		return "sp"
	case "enter":
		return "↵"
	case "esc":
		return "esc"
	case "tab":
		return "tab"
	case "shift+tab":
		return "⇤"
	case "up", "down", "left", "right":
		return map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→"}[key]
	}
	if strings.HasPrefix(key, "ctrl+") {
		return "^" + strings.TrimPrefix(key, "ctrl+")
	}
	return key
}

// fmtHints builds a help line from alternating key/desc pairs.
func fmtHints(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(helpStyle.Render(" · "))
		}
		b.WriteString(fmtKey(displayKey(pairs[i]), pairs[i+1]))
	}
	return b.String()
}

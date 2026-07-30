package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/ghx/internal/gh"
)

// Acting on a PR needs three things: which repo, which number, and a client
// scoped to that repo. The detail view holds them, but the list view has only a
// row — so the target is modelled as a value both can produce, and the actions
// take it rather than reaching into a view.

// actionTarget identifies the PR an action applies to.
type actionTarget struct {
	number int
	repo   string // "owner/name"
	title  string // for confirmation text; not used in requests
	isDraft bool
	state  string // OPEN | CLOSED | MERGED
}

func (t actionTarget) valid() bool { return t.number > 0 }

// label renders the target for a prompt, e.g. `#842 in keyolk/ghx`.
func (t actionTarget) label() string {
	if t.repo == "" {
		return fmt.Sprintf("#%d", t.number)
	}
	return fmt.Sprintf("#%d in %s", t.number, t.repo)
}

// client returns a client scoped to the target's repo. ghx runs outside any
// checkout, so an unscoped client would try to infer the repo from the cwd.
func (a *App) clientFor(t actionTarget) *gh.Client {
	if t.repo == "" {
		return a.client
	}
	return a.client.WithRepo(t.repo)
}

// currentTarget returns the PR the user is looking at: the open detail view, or
// the selected list row.
func (a *App) currentTarget() (actionTarget, bool) {
	if a.state == viewPRDetail && a.detail != nil {
		t := actionTarget{number: a.detail.number}
		if a.detail.owner != "" && a.detail.repo != "" {
			t.repo = a.detail.owner + "/" + a.detail.repo
		}
		if d := a.detail.detail; d != nil {
			t.title, t.isDraft, t.state = d.Title, d.IsDraft, d.State
		}
		return t, true
	}
	if a.list != nil {
		if item, ok := a.list.selectedItem(); ok {
			p := item.pr
			return actionTarget{
				number: p.Number, repo: p.Repo, title: p.Title,
				isDraft: p.IsDraft, state: p.State,
			}, true
		}
	}
	return actionTarget{}, false
}

// --- confirmation ---

// confirmKind names the pending action so the prompt can describe it and the
// handler can dispatch it.
type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmApprove
	confirmClose
	confirmReopen
	confirmReady
)

// confirmPrompt gates an action behind a yes/no. In the list a stray keypress
// while scrolling would otherwise approve or close whichever PR happened to be
// selected, which is not something the user can take back.
type confirmPrompt struct {
	kind   confirmKind
	target actionTarget
}

func (a *App) askConfirm(kind confirmKind, t actionTarget) tea.Cmd {
	if !t.valid() {
		return errCmd(fmt.Errorf("no pull request selected"))
	}
	a.confirm = &confirmPrompt{kind: kind, target: t}
	return nil
}

// handleConfirmKey resolves the prompt. Only an explicit y proceeds.
func (a *App) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	p := a.confirm
	switch msg.String() {
	case "y", "Y":
		a.confirm = nil
		return a.runConfirmed(p.kind, p.target)
	case "n", "N", "esc", "q":
		a.confirm = nil
		return nil
	}
	// Anything else is ignored rather than treated as consent.
	return nil
}

func (a *App) runConfirmed(kind confirmKind, t actionTarget) tea.Cmd {
	client := a.clientFor(t)
	n := t.number
	switch kind {
	case confirmApprove:
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return reviewPostedMsg{action: "approve", err: client.ReviewPR(c, n, "approve", "")}
		}
	case confirmClose:
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return actionDoneMsg{label: fmt.Sprintf("closed #%d", n), err: client.Close(c, n, "")}
		}
	case confirmReopen:
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return actionDoneMsg{label: fmt.Sprintf("reopened #%d", n), err: client.Reopen(c, n)}
		}
	case confirmReady:
		undo := !t.isDraft
		verb := "marked ready"
		if undo {
			verb = "converted to draft"
		}
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return actionDoneMsg{
				label: fmt.Sprintf("%s #%d", verb, n),
				err:   client.Ready(c, n, undo),
			}
		}
	}
	return nil
}

// renderConfirm draws the prompt, naming the exact PR so the user can catch a
// mis-selected row before acting on it.
func (a *App) renderConfirm(width, height int) string {
	p := a.confirm
	var question, note string
	switch p.kind {
	case confirmApprove:
		question = "Approve " + p.target.label() + "?"
	case confirmClose:
		question = "Close " + p.target.label() + "?"
		note = "The PR stays on GitHub and can be reopened."
	case confirmReopen:
		question = "Reopen " + p.target.label() + "?"
	case confirmReady:
		if p.target.isDraft {
			question = "Mark " + p.target.label() + " ready for review?"
		} else {
			question = "Convert " + p.target.label() + " back to a draft?"
		}
	}

	var b strings.Builder
	b.WriteString(question + "\n")
	if p.target.title != "" {
		b.WriteString(dimStyle.Render(fitCell(p.target.title, max(width-8, 20))) + "\n")
	}
	if note != "" {
		b.WriteString(dimStyle.Render(note) + "\n")
	}
	b.WriteString("\n" + fmtHints("y", "yes", "n", "no"))
	return decoratedPane("confirm", b.String(), min(width-4, 76), 8, true)
}

// --- label picker ---

// labelPicker lets the user toggle labels on the selected PR. It loads the
// repo's labels lazily: the list is per-repo and a cross-repo queue would
// otherwise fetch dozens of label sets nobody asked for.
type labelPicker struct {
	target  actionTarget
	all     []gh.RepoLabel
	applied map[string]bool
	// pending holds edits not yet submitted, so the picker can show the result
	// before committing and submit adds and removes in one pass.
	pending map[string]bool
	cursor  int
	offset  int
	loading bool
	err     error
	query   string
}

func (a *App) openLabelPicker(t actionTarget) tea.Cmd {
	if !t.valid() {
		return errCmd(fmt.Errorf("no pull request selected"))
	}
	a.labels = &labelPicker{
		target:  t,
		applied: map[string]bool{},
		pending: map[string]bool{},
		loading: true,
	}
	client := a.clientFor(t)
	n := t.number
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		all, err := client.RepoLabels(c)
		if err != nil {
			return labelsLoadedMsg{err: err}
		}
		on, err := client.PRLabels(c, n)
		if err != nil {
			return labelsLoadedMsg{err: err}
		}
		return labelsLoadedMsg{all: all, applied: on}
	}
}

// visible returns the labels matching the filter.
func (p *labelPicker) visible() []gh.RepoLabel {
	if p.query == "" {
		return p.all
	}
	q := strings.ToLower(p.query)
	out := make([]gh.RepoLabel, 0, len(p.all))
	for _, l := range p.all {
		if strings.Contains(strings.ToLower(l.Name), q) ||
			strings.Contains(strings.ToLower(l.Description), q) {
			out = append(out, l)
		}
	}
	return out
}

// isOn reports the label's state including any un-submitted toggle.
func (p *labelPicker) isOn(name string) bool {
	if v, ok := p.pending[name]; ok {
		return v
	}
	return p.applied[name]
}

// dirty reports whether there is anything to submit.
func (p *labelPicker) dirty() bool {
	for name, want := range p.pending {
		if want != p.applied[name] {
			return true
		}
	}
	return false
}

// diff splits the pending edits into labels to add and to remove.
func (p *labelPicker) diff() (add, remove []string) {
	for name, want := range p.pending {
		switch {
		case want && !p.applied[name]:
			add = append(add, name)
		case !want && p.applied[name]:
			remove = append(remove, name)
		}
	}
	return add, remove
}

func (a *App) handleLabelKey(msg tea.KeyMsg) tea.Cmd {
	p := a.labels
	if p.loading {
		if msg.String() == "esc" {
			a.labels = nil
		}
		return nil
	}
	switch msg.String() {
	case "esc":
		a.labels = nil
		return nil
	case "enter":
		return a.submitLabels()
	case "up", "k", "ctrl+p":
		if n := len(p.visible()); n > 0 {
			p.cursor = clamp(p.cursor-1, 0, n-1)
		}
		return nil
	case "down", "j", "ctrl+n":
		if n := len(p.visible()); n > 0 {
			p.cursor = clamp(p.cursor+1, 0, n-1)
		}
		return nil
	case " ", "tab":
		vis := p.visible()
		if p.cursor < len(vis) {
			name := vis[p.cursor].Name
			p.pending[name] = !p.isOn(name)
		}
		return nil
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.cursor = 0
		}
		return nil
	}
	// Typing filters. j/k are consumed above for navigation, so a name
	// containing them is still reachable by typing the rest of it.
	if msg.Type == tea.KeyRunes {
		p.query += string(msg.Runes)
		p.cursor = 0
	}
	return nil
}

// submitLabels applies the pending toggles. Adds and removes go in separate
// calls because gh models them as distinct flags.
func (a *App) submitLabels() tea.Cmd {
	p := a.labels
	if !p.dirty() {
		a.labels = nil
		return nil
	}
	add, remove := p.diff()
	client := a.clientFor(p.target)
	n := p.target.number
	a.labels = nil
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if len(add) > 0 {
			if err := client.AddLabels(c, n, add); err != nil {
				return actionDoneMsg{label: "label", err: err}
			}
		}
		if len(remove) > 0 {
			if err := client.RemoveLabels(c, n, remove); err != nil {
				return actionDoneMsg{label: "label", err: err}
			}
		}
		return actionDoneMsg{
			label: fmt.Sprintf("labels updated on #%d (+%d -%d)", n, len(add), len(remove)),
		}
	}
}

func (a *App) renderLabelPicker(width, height int) string {
	p := a.labels
	boxW := min(width-4, 70)
	boxH := min(max(height/2, 8), 18)

	if p.loading {
		return decoratedPane("labels",
			renderSpinner(a.spinnerFrame, "Loading labels…"), boxW, 5, true)
	}
	if p.err != nil {
		return decoratedPane("labels",
			errorStyle.Render(p.err.Error())+"\n\n"+fmtHints("esc", "close"),
			boxW, 6, true)
	}

	vis := p.visible()
	var b strings.Builder
	prompt := helpKeyStyle.Render("filter: ") + p.query + blockCursor()
	b.WriteString(prompt + "\n")

	rows := max(boxH-5, 1)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	p.offset = clamp(p.offset, 0, max(len(vis)-rows, 0))

	if len(vis) == 0 {
		b.WriteString(dimStyle.Render("no labels match") + "\n")
	}
	end := min(p.offset+rows, len(vis))
	for i := p.offset; i < end; i++ {
		l := vis[i]
		mark := " "
		if p.isOn(l.Name) {
			mark = iconCheck
		}
		if i == p.cursor {
			// Plain text under the theme: the description is dimmed, and a
			// background over that would break at its reset code.
			line := fmt.Sprintf("%s %s", mark, l.Name)
			if l.Description != "" {
				line += "  " + l.Description
			}
			line = padCell(fitCell(line, boxW-4), boxW-4)
			b.WriteString(selectedRowStyle.Render(line) + "\n")
			continue
		}
		line := fmt.Sprintf("%s %s", mark, l.Name)
		if l.Description != "" {
			line += dimStyle.Render("  " + l.Description)
		}
		b.WriteString(fitCell(line, boxW-4) + "\n")
	}

	hint := fmtHints("sp", "toggle", "enter", "apply", "esc", "cancel")
	if p.dirty() {
		add, remove := p.diff()
		hint = diffHunkStyle.Render(fmt.Sprintf("+%d -%d ", len(add), len(remove))) + hint
	}
	b.WriteString("\n" + hint)
	return decoratedPane("labels · "+p.target.label(), b.String(), boxW, boxH, true)
}

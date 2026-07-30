package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/ghx/internal/config"
	"github.com/keyolk/ghx/internal/gh"
	"github.com/keyolk/ghx/internal/pr"
)

// prListModel is the PR list: source tabs across the top, the list on the left,
// a summary preview on the right.
type prListModel struct {
	cfg    *config.Config
	client *gh.Client
	km     *Keymap

	sources []config.SourceDef
	curTab  int

	// detectedRepo is the repository the launch directory (or the current tmux
	// window) belongs to. Kept so the tab strip can mark which tab came from
	// where the user is, rather than looking like an arbitrary first entry.
	detectedRepo string

	list *list.Model
	pane *SplitPane

	// per-source cache so switching tabs is instant after the first load
	caches   [][]pr.Summary
	loadings []bool
	// errs keeps the last failure per source so an empty pane can say why it is
	// empty instead of implying the source has no PRs.
	errs     []error
	inFlight bool

	// query filters the current source's rows client-side
	query string

	missStreak int
	stale      bool

	spinner int

	width  int
	height int
}

// prListItem wraps a PR for bubbles/list.
type prListItem struct{ pr pr.Summary }

func (i prListItem) FilterValue() string {
	return fmt.Sprintf("#%d %s %s", i.pr.Number, i.pr.Title, i.pr.Author.Login)
}

func newPRListModel(cfg *config.Config, client *gh.Client, km *Keymap) *prListModel {
	return newPRListModelWithRepo(cfg, client, km, "")
}

// newPRListModelWithRepo builds the list with detectedRepo leading the tabs, so
// launching ghx inside a checkout opens on that repository's PRs.
func newPRListModelWithRepo(cfg *config.Config, client *gh.Client, km *Keymap, detectedRepo string) *prListModel {
	sources := cfg.EffectiveSources(detectedRepo)
	if len(sources) == 0 {
		sources = []config.SourceDef{
			{Name: "My reviews", Query: "review-requested:@me state:open"},
		}
	}
	m := &prListModel{
		cfg:          cfg,
		client:       client,
		km:           km,
		sources:      sources,
		detectedRepo: detectedRepo,
		caches:       make([][]pr.Summary, len(sources)),
		loadings:     make([]bool, len(sources)),
		errs:         make([]error, len(sources)),
	}
	l := list.New(nil, prListDelegate{}, 80, 20)
	initListBase(&l)
	configureListSearch(&l)
	m.list = &l
	m.pane = &SplitPane{List: m.list, ItemHeight: 1}
	return m
}

func (m *prListModel) init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.sources))
	for i := range m.sources {
		m.loadings[i] = true
		cmds = append(cmds, m.fetchSource(i))
	}
	return tea.Batch(cmds...)
}

// fetchSource loads one source off the UI goroutine.
//
// A source pinned to one repo goes through `gh pr list`, which is REST and has
// its own generous rate limit. Only an unpinned source needs `gh search prs`,
// which spans repositories but spends the much scarcer GraphQL search budget.
func (m *prListModel) fetchSource(i int) tea.Cmd {
	if i < 0 || i >= len(m.sources) {
		return nil
	}
	src := m.sources[i]
	client := m.client
	if src.Repo != "" {
		scoped := client.WithRepo(src.Repo)
		query, repo := src.Query, src.Repo
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			prs, err := scoped.ListPRs(c, query, 50)
			// ListPRs answers about one repo, so it never echoes the slug back;
			// fill it in or every later call would have nothing to scope to.
			for j := range prs {
				if prs[j].Repo == "" {
					prs[j].Repo = repo
				}
			}
			return prListMsg{sourceIdx: i, prs: prs, err: err}
		}
	}
	query := src.Query
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prs, err := client.SearchPRs(c, query, 50)
		return prListMsg{sourceIdx: i, prs: prs, err: err}
	}
}

// refreshCurrent reloads the visible source.
func (m *prListModel) refreshCurrent() tea.Cmd {
	m.loadings[m.curTab] = true
	return m.fetchSource(m.curTab)
}

func (m *prListModel) loading() bool {
	for _, l := range m.loadings {
		if l {
			return true
		}
	}
	return false
}

func (m *prListModel) resize(w, h int) {
	m.width, m.height = w, h
	// One row goes to the tab strip.
	m.pane.Resize(w, h-1, m.cfg.DiffSplitRatio)
}

func (m *prListModel) setSpinnerFrame(f int) { m.spinner = f }

func (m *prListModel) handlePRListMsg(msg prListMsg) tea.Cmd {
	if msg.sourceIdx < 0 || msg.sourceIdx >= len(m.sources) {
		return nil
	}
	m.loadings[msg.sourceIdx] = false
	m.inFlight = false
	if msg.err != nil {
		m.missStreak++
		// Only claim staleness after repeated failures — one blip isn't news.
		if m.missStreak >= 5 {
			m.stale = true
		}
		m.errs[msg.sourceIdx] = msg.err
		// Surface the first failure: an empty list that is really an error must
		// not read as "no PRs here".
		if m.missStreak == 1 {
			return errCmd(fmt.Errorf("%s: %w", m.sources[msg.sourceIdx].Name, msg.err))
		}
		return nil
	}
	m.missStreak = 0
	m.stale = false
	m.errs[msg.sourceIdx] = nil
	m.caches[msg.sourceIdx] = msg.prs
	if msg.sourceIdx == m.curTab {
		m.syncListItems()
	}
	return nil
}

// handlePollTick refreshes the current source, skipping if one is in flight.
func (m *prListModel) handlePollTick() tea.Cmd {
	if m.inFlight {
		return nil
	}
	m.inFlight = true
	return m.fetchSource(m.curTab)
}

// applyQuery sets the client-side filter and rebuilds the visible rows.
func (m *prListModel) applyQuery(q string) {
	m.query = q
	m.syncListItems()
}

// selectSourceByName switches tabs by (case-insensitive, prefix) name.
func (m *prListModel) selectSourceByName(name string) bool {
	if name == "" {
		return false
	}
	want := strings.ToLower(name)
	for i, s := range m.sources {
		ls := strings.ToLower(s.Name)
		if ls == want || strings.HasPrefix(ls, want) {
			m.selectTab(i)
			return true
		}
	}
	return false
}

func (m *prListModel) selectTab(i int) tea.Cmd {
	if i < 0 || i >= len(m.sources) || i == m.curTab {
		return nil
	}
	m.curTab = i
	m.syncListItems()
	// Load on first visit; cached sources render immediately.
	if m.caches[i] == nil && !m.loadings[i] {
		m.loadings[i] = true
		return m.fetchSource(i)
	}
	return nil
}

// syncListItems pushes the current source's rows (filtered) into the list.
func (m *prListModel) syncListItems() {
	src := m.caches[m.curTab]
	items := make([]list.Item, 0, len(src))
	for _, p := range src {
		if !matchesQuery(p, m.query) {
			continue
		}
		items = append(items, prListItem{pr: p})
	}
	setListItemsPreservingFilter(m.list, items)
}

func (m *prListModel) update(msg tea.KeyMsg) tea.Cmd {
	key, isNav := translateNav(msg)
	if isNav {
		switch key {
		case "up", "down", "pgup", "pgdown":
			// Let the list move the cursor; it handles bounds and paging.
			newL, cmd := m.list.Update(navKeyMsg(key))
			*m.list = newL
			return cmd
		case "home":
			m.list.Select(0)
			return nil
		case "end":
			if n := len(m.list.VisibleItems()); n > 0 {
				m.list.Select(n - 1)
			}
			return nil
		case "right":
			m.pane.Show = true
			m.pane.Focus = true
			m.pane.Resize(m.width, m.height-1, m.cfg.DiffSplitRatio)
			return nil
		case "left":
			if m.pane.Focus {
				m.pane.Focus = false
			} else {
				m.pane.Show = false
			}
			m.pane.Resize(m.width, m.height-1, m.cfg.DiffSplitRatio)
			return nil
		}
		return nil
	}

	switch key {
	case "enter":
		return func() tea.Msg { return openDetailMsg{} }
	case "R":
		return m.refreshCurrent()
	case "tab":
		return m.selectTab((m.curTab + 1) % len(m.sources))
	case "shift+tab":
		return m.selectTab((m.curTab - 1 + len(m.sources)) % len(m.sources))
	case "[":
		m.adjustRatio(-5)
		return nil
	case "]":
		m.adjustRatio(5)
		return nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.selectTab(int(key[0] - '1'))
	case "esc":
		// Esc clears an active filter before doing anything else.
		if m.query != "" {
			m.applyQuery("")
		}
		return nil
	}
	return nil
}

// navKeyMsg rebuilds a canonical key message for the list to consume.
func navKeyMsg(key string) tea.KeyMsg {
	switch key {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	return tea.KeyMsg{}
}

func (m *prListModel) adjustRatio(delta int) {
	r := m.cfg.DiffSplitRatio + delta
	// Keep both panes usable; a 10% pane shows nothing worth reading.
	m.cfg.DiffSplitRatio = clamp(r, 25, 75)
	m.pane.Resize(m.width, m.height-1, m.cfg.DiffSplitRatio)
}

func (m *prListModel) selectedItem() (prListItem, bool) {
	if m.list == nil {
		return prListItem{}, false
	}
	it, ok := m.list.SelectedItem().(prListItem)
	return it, ok
}

func (m *prListModel) title() string {
	if m.curTab >= len(m.sources) {
		return ""
	}
	s := m.sources[m.curTab].Name
	n := len(m.list.VisibleItems())
	total := len(m.caches[m.curTab])
	if m.query != "" {
		return fmt.Sprintf("%s · %d/%d matching %q", s, n, total, m.query)
	}
	return fmt.Sprintf("%s · %d", s, n)
}

func (m *prListModel) view(w, h int) string {
	tabs := m.renderTabs(w)
	if m.pane.Show {
		m.renderPreview()
	}
	// An empty pane must distinguish loading, a failed query, an over-narrow
	// filter, and a genuinely empty source — they call for different actions.
	if len(m.list.VisibleItems()) == 0 {
		var body string
		switch {
		case m.loadings[m.curTab]:
			body = renderSpinner(m.spinner, "Loading "+m.sources[m.curTab].Name+"…")
		case m.errs[m.curTab] != nil:
			body = errorStyle.Render("Could not load this source:") + "\n  " +
				dimStyle.Render(m.errs[m.curTab].Error()) + "\n\n" +
				dimStyle.Render("R retries.")
		case m.query != "":
			body = dimStyle.Render(fmt.Sprintf("No PRs match %q — esc clears the filter.", m.query))
		default:
			body = dimStyle.Render("No PRs in this source.")
		}
		return joinVertical(tabs, body)
	}
	return joinVertical(tabs, m.pane.Render(w, h-1, m.cfg.DiffSplitRatio))
}

// renderTabs draws the source strip, shrinking labels as width runs out.
func (m *prListModel) renderTabs(w int) string {
	// Measure the full-label strip; if it doesn't fit, fall back to short forms.
	full := m.tabStrip(w, false)
	if lipglossWidth(full) <= w {
		return full
	}
	return m.tabStrip(w, true)
}

func (m *prListModel) tabStrip(w int, short bool) string {
	var b strings.Builder
	for i, s := range m.sources {
		name := s.Name
		if short {
			// Keep the digit and just enough of the name to tell tabs apart.
			if len(name) > 3 {
				name = name[:3]
			}
		}
		label := fmt.Sprintf("%d %s", i+1, name)
		// Mark the tab that came from where the user is, so leading with it reads
		// as deliberate rather than as an arbitrary first entry.
		if !short && m.detectedRepo != "" && strings.EqualFold(s.Repo, m.detectedRepo) {
			label += tabCountStyle.Render("*")
		}
		if n := len(m.caches[i]); n > 0 && !short {
			label += tabCountStyle.Render(fmt.Sprintf("(%d)", n))
		}
		if i == m.curTab {
			b.WriteString(tabActiveStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(tabDimStyle.Render(" " + label + " "))
		}
	}
	if m.stale {
		b.WriteString(checkFailStyle.Render(" stale"))
	}
	out, _ := truncateExact(b.String(), w)
	return out
}

// renderPreview fills the right pane with the selected PR's summary.
func (m *prListModel) renderPreview() {
	it, ok := m.selectedItem()
	if !ok {
		m.pane.SetPreviewContent(dimStyle.Render("No selection."),
			m.width, m.height-1, m.cfg.DiffSplitRatio)
		return
	}
	p := it.pr
	var b strings.Builder
	b.WriteString(prTitleStyle.Render(p.Title) + "\n\n")
	b.WriteString(field("PR", prNumberStyle.Render(fmt.Sprintf("#%d", p.Number))))
	if p.Repo != "" {
		b.WriteString(field("Repo", p.Repo))
	}
	b.WriteString(field("Author", prAuthorStyle.Render(p.Author.Login)))
	state := p.State
	if p.IsDraft {
		state += prDraftStyle.Render(" (draft)")
	}
	b.WriteString(field("State", state))
	b.WriteString(field("Review", reviewDecisionLabel(p.ReviewDecision)))
	b.WriteString(field("Branch", p.HeadRefName))
	b.WriteString(field("Updated", relTime(p.UpdatedAt)+" ago"))
	b.WriteString("\n" + dimStyle.Render("enter opens the full PR"))
	m.pane.SetPreviewContent(b.String(), m.width, m.height-1, m.cfg.DiffSplitRatio)
}

func reviewDecisionLabel(d string) string {
	switch d {
	case "APPROVED":
		return prApprovedStyle.Render("approved")
	case "CHANGES_REQUESTED":
		return prChangesStyle.Render("changes requested")
	case "REVIEW_REQUIRED":
		return prRequiredStyle.Render("review required")
	case "":
		return dimStyle.Render("—")
	}
	return d
}

func (m *prListModel) helpLine() string {
	return fmtHints(
		"enter", "open",
		"a", "approve",
		"x", "close",
		"L", "labels",
		"r", "request",
		"o", "browser",
		"/", "filter",
		":", "palette",
		"?", "help",
	)
}

package tui

import (
	"strings"
	"testing"

	"github.com/keyolk/ghx/internal/pr"
)

// A review comment is prose: far longer than a diff column is wide, and about
// one side of the change only. Rendering it across both halves pushes the
// opposite side's code out of view, and truncating it to a single line hides
// the finding the reviewer has to read.

const wrapDiff = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,3 @@
 keep
-removed
+added
 tail
`

func longComment() string {
	return strings.Repeat(
		"Hardcoded app roster diverges from the existing source of truth. ", 4)
}

func viewWithComment(t *testing.T, side string, line int) *diffView {
	t.Helper()
	v := newDiffView()
	threads := []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: line, DiffSide: side,
		Comments: []pr.ThreadComment{{
			Body: longComment(), Author: pr.User{Login: "claude"},
		}},
	}}
	if err := v.setContent(wrapDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	v.sideBySide = true
	return v
}

// The comment occupies its own column and leaves the other one alone.
func TestCommentOccupiesOnlyItsOwnColumn(t *testing.T) {
	cases := []struct {
		side     string
		line     int
		wantSide halfSide
	}{
		{"RIGHT", 11, sideRight},
		{"LEFT", 11, sideLeft},
	}
	for _, c := range cases {
		t.Run(c.side, func(t *testing.T) {
			v := viewWithComment(t, c.side, c.line)
			var seen bool
			for _, p := range v.sideRowsWidth(60) {
				idx, which, ok := p.threadHalf(v)
				if !ok {
					continue
				}
				seen = true
				if which != c.wantSide {
					t.Errorf("comment landed in the %v column, want %v", which, c.wantSide)
				}
				// The opposite half must be blank, not occupied by the comment.
				if which == sideRight && p.left >= 0 {
					t.Error("a RIGHT comment must leave the left column free")
				}
				if which == sideLeft && p.right >= 0 {
					t.Error("a LEFT comment must leave the right column free")
				}
				_ = idx
			}
			if !seen {
				t.Fatalf("no one-sided comment row for %s", c.side)
			}
		})
	}
}

// A long comment spans as many screen rows as its column needs; it must not be
// cut off at one line.
func TestLongCommentWrapsAcrossScreenRows(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)

	narrow := v.sideRowsWidth(40)
	wide := v.sideRowsWidth(120)

	count := func(pairs []sidePair) int {
		n := 0
		for _, p := range pairs {
			if _, _, ok := p.threadHalf(v); ok {
				n++
			}
		}
		return n
	}
	nNarrow, nWide := count(narrow), count(wide)
	if nNarrow < 2 {
		t.Errorf("a long comment in a 40-cell column takes %d rows, want more than 1", nNarrow)
	}
	// A wider column needs fewer rows for the same text.
	if nWide >= nNarrow {
		t.Errorf("widening the column did not reduce the wrapped rows (%d then %d)",
			nNarrow, nWide)
	}
	// The wrap indices have to be sequential, or the renderer would repeat or
	// skip parts of the comment.
	want := 0
	for _, p := range narrow {
		if _, _, ok := p.threadHalf(v); !ok {
			continue
		}
		if p.wrapLine != want {
			t.Errorf("wrap line = %d, want %d", p.wrapLine, want)
		}
		want++
	}
}

// Every part of the comment must actually appear on screen.
func TestWrappedCommentTextIsFullyRendered(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)
	out := v.renderSideBySide(140, 20)
	plain := stripANSISeqs(out)

	// Pick words from the start and end of the body; both must survive wrapping.
	if !strings.Contains(plain, "Hardcoded app roster") {
		t.Errorf("the start of the comment is missing:\n%s", plain)
	}
	if !strings.Contains(plain, "source of truth") {
		t.Errorf("the end of the comment is missing — it was truncated:\n%s", plain)
	}
}

// The comment's rows must not spill into the code column: the left half of a
// RIGHT comment's rows has to be blank.
func TestWrappedCommentDoesNotOverwriteOppositeColumn(t *testing.T) {
	forceColor(t)
	v := viewWithComment(t, "RIGHT", 11)
	out := v.renderSideBySide(140, 20)

	for _, line := range strings.Split(out, "\n") {
		plain := stripANSISeqs(line)
		if !strings.Contains(plain, "Hardcoded") && !strings.Contains(plain, "source of truth") {
			continue
		}
		halves := strings.SplitN(plain, "│", 2)
		if len(halves) != 2 {
			t.Fatalf("comment row is not split by the divider: %q", plain)
		}
		if strings.TrimSpace(halves[0]) != "" {
			t.Errorf("the left column should be blank on a RIGHT comment row, got %q",
				halves[0])
		}
	}
}

// Every rendered row must still fit the terminal exactly, or the divider drifts
// and the two sides stop lining up.
func TestWrappedCommentRowsFitWidth(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)
	for _, w := range []int{80, 120, 200} {
		out := v.renderSideBySide(w, 25)
		for i, line := range strings.Split(out, "\n") {
			if got := lipglossWidth(line); got > w {
				t.Errorf("width %d: row %d renders %d cells", w, i, got)
			}
		}
	}
}

// j must step past the whole comment, not once per wrapped line: the wrapped
// rows are one comment, and stopping on each would make navigation crawl.
func TestNavigationStepsOverAWrappedComment(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)
	for i, r := range v.rows {
		if r.kind == rowThread {
			v.cursor = i
			break
		}
	}
	v.syncSideFocus()
	before := v.cursor

	v.moveDown(1)

	if v.cursor == before {
		t.Fatal("j did not move off the comment")
	}
	if v.rows[v.cursor].kind == rowThread {
		t.Error("j should step past the comment, not onto another of its wrapped lines")
	}
}

// Comments on both sides at the same anchor keep to their own columns.
func TestCommentsOnBothSidesStaySeparate(t *testing.T) {
	v := newDiffView()
	threads := []pr.ReviewThread{
		{ID: "L", Path: "app.go", Line: 11, DiffSide: "LEFT",
			Comments: []pr.ThreadComment{{Body: "about the old line"}}},
		{ID: "R", Path: "app.go", Line: 11, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "about the new line"}}},
	}
	if err := v.setContent(wrapDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	v.sideBySide = true

	sides := map[string]halfSide{}
	for _, p := range v.sideRowsWidth(60) {
		idx, which, ok := p.threadHalf(v)
		if !ok {
			continue
		}
		sides[v.rows[idx].threadID] = which
	}
	if sides["L"] != sideLeft {
		t.Errorf("LEFT comment is in the %v column", sides["L"])
	}
	if sides["R"] != sideRight {
		t.Errorf("RIGHT comment is in the %v column", sides["R"])
	}
}

// Threads listed under a file header have no column of their own (their anchor
// is not in the diff), so they may span the full width.
func TestOrphanThreadSpansFullWidth(t *testing.T) {
	v := newDiffView()
	threads := []pr.ReviewThread{{
		ID: "O", Path: "app.go", Line: 9999, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "anchor is gone"}},
	}}
	if err := v.setContent(wrapDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	v.sideBySide = true
	out := v.renderSideBySide(140, 20)
	if !strings.Contains(stripANSISeqs(out), "anchor is gone") {
		t.Errorf("an orphan comment must still be visible:\n%s", stripANSISeqs(out))
	}
}

// The unified layout has the same problem: a comment on one line is longer than
// the pane, and truncating it hides the finding.
func TestUnifiedLayoutWrapsComments(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)
	v.sideBySide = false

	out := v.render(100, 25)
	plain := stripANSISeqs(out)

	if !strings.Contains(plain, "Hardcoded app roster") {
		t.Errorf("the start of the comment is missing:\n%s", plain)
	}
	// The body repeats the phrase four times; the last one only survives if the
	// text wrapped rather than being cut at the pane edge.
	if strings.Count(plain, "source of truth") < 2 {
		t.Errorf("the comment was truncated instead of wrapped:\n%s", plain)
	}

	// Continuation lines are indented past the marker so the comment reads as one
	// block, and no line may exceed the pane.
	for _, line := range strings.Split(out, "\n") {
		if got := lipglossWidth(line); got > 100 {
			t.Errorf("line renders %d cells, want at most 100: %q", got, stripANSISeqs(line))
		}
	}
}

// Wrapping must not push the pane past its height budget, or the footer would be
// scrolled off the screen.
func TestUnifiedWrapRespectsHeight(t *testing.T) {
	v := viewWithComment(t, "RIGHT", 11)
	v.sideBySide = false

	const height = 6
	out := v.render(80, height)
	if got := len(strings.Split(out, "\n")); got > height {
		t.Errorf("render produced %d lines for height %d", got, height)
	}
}

// A short comment needs no wrapping and should stay on one line.
func TestShortCommentStaysOnOneLine(t *testing.T) {
	v := newDiffView()
	threads := []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "nit", Author: pr.User{Login: "a"}}},
	}}
	if err := v.setContent(wrapDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(v.render(120, 20), "\n") {
		if strings.Contains(stripANSISeqs(line), "nit") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("a short comment occupies %d lines, want 1", lines)
	}
}

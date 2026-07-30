package tui

import (
	"strings"
	"testing"

	"github.com/keyolk/ghx/internal/pr"
)

// sideRowsFor is a helper: load a diff and return its paired screen rows.
func sideRowsFor(t *testing.T, raw string) (*diffView, []sidePair) {
	t.Helper()
	v := newDiffView()
	if err := v.setContent(raw, nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	return v, v.sideRows()
}

// describe renders a pair as "old|new" line numbers for readable assertions.
func describe(v *diffView, p sidePair) string {
	half := func(idx int, right bool) string {
		if idx < 0 {
			return "-"
		}
		r := v.rows[idx]
		if r.kind != rowDiffLine {
			return "hdr"
		}
		n := r.line.OldLineNo
		if right {
			n = r.line.NewLineNo
		}
		if n == 0 {
			return "0"
		}
		return lineNoStr(n)
	}
	return half(p.left, false) + "|" + half(p.right, true)
}

// A modification is a deletion block followed by an addition block. Those must
// zip so the old and new text of one edit share a screen row — that pairing is
// the whole point of the layout.
func TestSideBySidePairsModifiedLines(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,3 @@
 keep
-old one
-old two
+new one
+new two
`
	v, pairs := sideRowsFor(t, raw)
	var got []string
	for _, p := range pairs {
		got = append(got, describe(v, p))
	}
	// file header, hunk header, context, then two zipped changes.
	want := []string{"hdr|hdr", "hdr|hdr", "10|10", "11|11", "12|12"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("pairs = %v\nwant     %v", got, want)
	}
}

// A pure insertion has nothing on the left; the left half must stay blank so
// the right column's line numbers keep their alignment.
func TestSideBySidePureInsertionLeavesLeftBlank(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,1 +10,3 @@
 keep
+added one
+added two
`
	v, pairs := sideRowsFor(t, raw)
	var changes []string
	for _, p := range pairs {
		if p.left == p.right {
			continue // headers and context
		}
		changes = append(changes, describe(v, p))
	}
	want := []string{"-|11", "-|12"}
	if strings.Join(changes, " ") != strings.Join(want, " ") {
		t.Errorf("insertion pairs = %v, want %v", changes, want)
	}
}

func TestSideBySidePureDeletionLeavesRightBlank(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,1 @@
 keep
-gone one
-gone two
`
	v, pairs := sideRowsFor(t, raw)
	var changes []string
	for _, p := range pairs {
		if p.left == p.right {
			continue
		}
		changes = append(changes, describe(v, p))
	}
	want := []string{"11|-", "12|-"}
	if strings.Join(changes, " ") != strings.Join(want, " ") {
		t.Errorf("deletion pairs = %v, want %v", changes, want)
	}
}

// When the blocks are uneven, the surplus lines must still appear — dropping the
// leftovers would hide real changes from the reviewer.
func TestSideBySideUnevenBlocksKeepEveryLine(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,4 +10,3 @@
-old one
-old two
-old three
+new one
`
	v, pairs := sideRowsFor(t, raw)
	var lefts, rights int
	for _, p := range pairs {
		if p.left == p.right {
			continue
		}
		if p.left >= 0 {
			lefts++
		}
		if p.right >= 0 {
			rights++
		}
	}
	if lefts != 3 {
		t.Errorf("left lines = %d, want 3 (no deletion may be dropped)", lefts)
	}
	if rights != 1 {
		t.Errorf("right lines = %d, want 1", rights)
	}
	// The single addition pairs with the first deletion; the rest stand alone.
	first := describe(v, pairs[2])
	if first != "10|10" {
		t.Errorf("first change pair = %s, want 10|10", first)
	}
}

// Pairing must not reach across a hunk boundary: the line counters jump there,
// so zipping a deletion from one hunk with an addition from the next would show
// two unrelated lines as one edit.
func TestSideBySideDoesNotZipAcrossHunks(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,2 +10,1 @@
 keep
-only deletion
@@ -50,1 +50,2 @@
 keep
+only addition
`
	v, pairs := sideRowsFor(t, raw)
	for _, p := range pairs {
		if p.left < 0 || p.right < 0 || p.left == p.right {
			continue
		}
		l, r := v.rows[p.left], v.rows[p.right]
		if l.hunkIdx != r.hunkIdx {
			t.Errorf("paired rows from hunks %d and %d", l.hunkIdx, r.hunkIdx)
		}
	}
	// Both changed lines must still be present, each alone on its row.
	var solo int
	for _, p := range pairs {
		if p.left == p.right {
			continue
		}
		if (p.left >= 0) != (p.right >= 0) {
			solo++
		}
	}
	if solo != 2 {
		t.Errorf("standalone changes = %d, want 2", solo)
	}
}

// A comment anchored to a line belongs beside that line, in that line's column.
// Spanning both halves would push the opposite side's code out of view for no
// reason — the comment is about one side only.
func TestSideBySideThreadSitsInItsOwnColumn(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,2 +10,2 @@
-removed
+added
`
	threads := []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 10, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "look here"}},
	}}
	v := newDiffView()
	if err := v.setContent(raw, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	pairs := v.sideRows()

	var found bool
	for _, p := range pairs {
		idx := p.right
		if idx < 0 || v.rows[idx].kind != rowThread {
			continue
		}
		found = true
		if p.left >= 0 {
			t.Error("a RIGHT-anchored comment must leave the left column free")
		}
	}
	if !found {
		t.Fatal("the thread was not placed in the right column")
	}

	// A LEFT-anchored comment goes in the left column.
	threads[0].DiffSide = "LEFT"
	threads[0].Line = 10
	v2 := newDiffView()
	if err := v2.setContent(raw, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	found = false
	for _, p := range v2.sideRows() {
		if p.left >= 0 && v2.rows[p.left].kind == rowThread {
			found = true
			if p.right >= 0 {
				t.Error("a LEFT-anchored comment must leave the right column free")
			}
		}
	}
	if !found {
		t.Fatal("the thread was not placed in the left column")
	}
}

// Switching layout must not move the cursor: the comment target is derived from
// it, so a shifted cursor would silently retarget the comment.
func TestLayoutToggleKeepsCommentTarget(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,2 +10,2 @@
-old
+new
`
	v, _ := sideRowsFor(t, raw)
	cursorToLine(t, v, "RIGHT", 10)
	beforePath, beforeSide, beforeLine, _, ok := v.commentTarget()
	if !ok {
		t.Fatal("commentTarget: not ok before toggle")
	}

	v.sideBySide = true
	afterPath, afterSide, afterLine, _, ok := v.commentTarget()
	if !ok {
		t.Fatal("commentTarget: not ok after toggle")
	}
	if beforePath != afterPath || beforeSide != afterSide || beforeLine != afterLine {
		t.Errorf("target changed across layouts: %s %s:%d → %s %s:%d",
			beforePath, beforeSide, beforeLine, afterPath, afterSide, afterLine)
	}
}

// Every screen row must fit the terminal exactly, or the divider column drifts
// and the two halves stop lining up.
func TestSideBySideRowsFitWidth(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,3 @@
 a line of context that is fairly long and will need truncating in narrow panes
-a deleted line that is also quite long and should be truncated cleanly here
+an added line that is likewise long enough to require truncation on the right
`
	v, _ := sideRowsFor(t, raw)
	v.sideBySide = true
	for _, w := range []int{80, 120, 200} {
		out := v.renderSideBySide(w, 10)
		for i, line := range strings.Split(out, "\n") {
			if got := lipglossWidth(line); got > w {
				t.Errorf("width %d: row %d renders %d cells", w, i, got)
			}
		}
	}
}

// Below the width floor the layout says so rather than drawing slivers, and it
// must not recurse back into render().
func TestSideBySideTooNarrowExplains(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,1 +10,1 @@
-x
+y
`
	v, _ := sideRowsFor(t, raw)
	v.sideBySide = true
	out := v.renderSideBySide(20, 5)
	if !contains(out, "too narrow") {
		t.Errorf("a too-narrow pane should explain itself, got: %s", out)
	}
}

// render() picks the layout; a narrow terminal must fall back to unified even
// when side-by-side is enabled, so the reviewer still sees the diff.
func TestRenderFallsBackToUnifiedWhenNarrow(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,1 +10,1 @@
-x
+y
`
	v, _ := sideRowsFor(t, raw)
	v.sideBySide = true
	out := v.render(60, 10)
	if contains(out, "too narrow") {
		t.Error("render() should use the unified layout at 60 cols, not refuse")
	}
	if !contains(out, "x") || !contains(out, "y") {
		t.Errorf("unified fallback should still show both lines: %s", out)
	}
}

// Scrolling in the paired view has its own offset; it must keep the cursor's
// screen row on screen even though one row can hold two diff rows.
func TestSideBySideScrollKeepsCursorVisible(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/app.go b/app.go\n--- a/app.go\n+++ b/app.go\n@@ -1,60 +1,60 @@\n")
	for i := 0; i < 30; i++ {
		b.WriteString("-old\n+new\n")
	}
	v, _ := sideRowsFor(t, b.String())
	v.sideBySide = true

	v.cursor = len(v.rows) - 1 // last diff row
	const height = 10
	v.renderSideBySide(120, height)

	pairs := v.sideRows()
	cp := v.cursorPairIndex(pairs)
	if cp < v.sideOffset || cp >= v.sideOffset+height {
		t.Errorf("cursor pair %d outside window [%d,%d)", cp, v.sideOffset, v.sideOffset+height)
	}
}

// Context lines must be drawn in both columns. Rendering them full-width (as
// headers are) would break the vertical alignment the layout exists to provide:
// the two sides would drift apart around every unchanged line.
func TestSideBySideContextAppearsInBothColumns(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,3 @@
 shared context
-old
+new
`
	v, _ := sideRowsFor(t, raw)
	v.sideBySide = true
	out := v.renderSideBySide(120, 10)

	var contextRow string
	for _, line := range strings.Split(out, "\n") {
		if contains(line, "shared context") {
			contextRow = line
			break
		}
	}
	if contextRow == "" {
		t.Fatal("context line not rendered")
	}
	// The divider marks the column boundary; a context row has to cross it.
	if !contains(contextRow, "│") {
		t.Errorf("context row is not split into columns: %q", contextRow)
	}
	// Its text belongs on both sides, so it appears twice.
	if n := countOccurrences(contextRow, "shared context"); n != 2 {
		t.Errorf("context text appears %d time(s), want 2 (once per column)", n)
	}
}

// Headers, by contrast, name the file or hunk and must not be duplicated.
func TestSideBySideHeadersSpanFullWidth(t *testing.T) {
	const raw = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,2 +10,2 @@
-old
+new
`
	v, _ := sideRowsFor(t, raw)
	v.sideBySide = true
	out := v.renderSideBySide(120, 10)

	for _, line := range strings.Split(out, "\n") {
		if contains(line, "app.go (+") {
			if n := countOccurrences(line, "app.go"); n != 1 {
				t.Errorf("file header duplicated across columns: %q", line)
			}
		}
		if contains(line, "@@") {
			if n := countOccurrences(line, "@@"); n != 2 {
				// "@@ -10,2 +10,2 @@" legitimately contains two "@@" markers.
				t.Errorf("hunk header looks duplicated: %q", line)
			}
		}
	}
}

func countOccurrences(haystack, needle string) int {
	return strings.Count(haystack, needle)
}

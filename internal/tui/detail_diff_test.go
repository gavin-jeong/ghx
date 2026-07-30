package tui

import (
	"testing"

	"github.com/keyolk/ghx/internal/pr"
)

// sampleDiff has one file, one hunk, with a deletion and two additions so both
// diff sides are exercised.
const sampleDiff = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,3 +10,4 @@
 context line
-removed line
+added one
+added two
`

func newLoadedDiff(t *testing.T, threads []pr.ReviewThread) *diffView {
	t.Helper()
	v := newDiffView()
	if err := v.setContent(sampleDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	return v
}

// cursorToLine parks the cursor on the diff row whose anchor matches.
func cursorToLine(t *testing.T, v *diffView, side string, line int) {
	t.Helper()
	for i, r := range v.rows {
		if r.kind == rowDiffLine && r.side == side && r.anchorLine == line {
			v.cursor = i
			return
		}
	}
	t.Fatalf("no diff row for %s:%d", side, line)
}

func TestCommentTargetSingleLine(t *testing.T) {
	v := newLoadedDiff(t, nil)
	cursorToLine(t, v, "RIGHT", 11) // "added one"

	path, side, line, startLine, ok := v.commentTarget()
	if !ok {
		t.Fatal("commentTarget: not ok on a diff line")
	}
	if path != "app.go" || side != "RIGHT" || line != 11 {
		t.Errorf("got %s %s:%d, want app.go RIGHT:11", path, side, line)
	}
	if startLine != 0 {
		t.Errorf("startLine = %d, want 0 for a single-line comment", startLine)
	}
}

func TestCommentTargetHeaderRowIsNotCommentable(t *testing.T) {
	v := newLoadedDiff(t, nil)
	v.cursor = 0 // file header
	if _, _, _, _, ok := v.commentTarget(); ok {
		t.Error("file header must not be commentable")
	}
}

// A visual selection must report the full span, and must do so no matter which
// direction the user dragged — this is what makes multi-line comments work.
func TestCommentTargetVisualRange(t *testing.T) {
	t.Run("downward", func(t *testing.T) {
		v := newLoadedDiff(t, nil)
		cursorToLine(t, v, "RIGHT", 11)
		v.visual = true
		v.visualStart = v.cursor
		cursorToLine(t, v, "RIGHT", 12)

		_, side, line, startLine, ok := v.commentTarget()
		if !ok {
			t.Fatal("commentTarget: not ok")
		}
		if side != "RIGHT" || line != 12 || startLine != 11 {
			t.Errorf("got %s start=%d line=%d, want RIGHT start=11 line=12",
				side, startLine, line)
		}
	})

	t.Run("upward", func(t *testing.T) {
		v := newLoadedDiff(t, nil)
		cursorToLine(t, v, "RIGHT", 12)
		v.visual = true
		v.visualStart = v.cursor
		cursorToLine(t, v, "RIGHT", 11)

		_, _, line, startLine, ok := v.commentTarget()
		if !ok {
			t.Fatal("commentTarget: not ok")
		}
		// Dragging upward still has to describe a real span; both ends present.
		if startLine == 0 {
			t.Fatal("startLine = 0, want the other end of the selection")
		}
		lo, hi := startLine, line
		if lo > hi {
			lo, hi = hi, lo
		}
		if lo != 11 || hi != 12 {
			t.Errorf("span = %d-%d, want 11-12", lo, hi)
		}
	})
}

// A selection spanning two sides can't be posted as one range, so it must fall
// back to a single-line comment rather than sending a bad request.
func TestCommentTargetVisualAcrossSidesFallsBack(t *testing.T) {
	v := newLoadedDiff(t, nil)
	cursorToLine(t, v, "LEFT", 11) // "removed line"
	v.visual = true
	v.visualStart = v.cursor
	cursorToLine(t, v, "RIGHT", 11)

	_, side, line, startLine, ok := v.commentTarget()
	if !ok {
		t.Fatal("commentTarget: not ok")
	}
	if side != "RIGHT" || line != 11 {
		t.Errorf("got %s:%d, want RIGHT:11", side, line)
	}
	if startLine != 0 {
		t.Errorf("startLine = %d, want 0 (mixed-side selection is not a range)", startLine)
	}
}

// Threads must be overlaid on the line they are anchored to, on the right side.
func TestThreadOverlayPlacement(t *testing.T) {
	threads := []pr.ReviewThread{{
		ID: "T1", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "why?", Author: pr.User{Login: "alice"}}},
	}}
	v := newLoadedDiff(t, threads)

	// The thread row must sit immediately after its anchor line.
	anchorIdx := -1
	for i, r := range v.rows {
		if r.kind == rowDiffLine && r.side == "RIGHT" && r.anchorLine == 11 {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		t.Fatal("anchor row not found")
	}
	if anchorIdx+1 >= len(v.rows) {
		t.Fatal("no row after the anchor")
	}
	next := v.rows[anchorIdx+1]
	if next.kind != rowThread || next.threadID != "T1" {
		t.Errorf("row after anchor = kind %v id %q, want a thread row for T1",
			next.kind, next.threadID)
	}
}

// An outdated thread reports line=0; it should still appear, via originalLine,
// rather than vanishing from the review.
func TestThreadOverlayFallsBackToOriginalLine(t *testing.T) {
	threads := []pr.ReviewThread{{
		ID: "T2", Path: "app.go", Line: 0, OriginalLine: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "stale", Author: pr.User{Login: "bob"}}},
	}}
	v := newLoadedDiff(t, threads)
	found := false
	for _, r := range v.rows {
		if r.kind == rowThread && r.threadID == "T2" {
			found = true
		}
	}
	if !found {
		t.Error("thread with line=0 must still render via originalLine")
	}
}

func TestFoldHidesHunksAndKeepsCursorOnHeader(t *testing.T) {
	v := newLoadedDiff(t, nil)
	before := len(v.rows)
	v.cursor = 0
	v.toggleFold()
	if len(v.rows) >= before {
		t.Errorf("rows = %d after fold, want fewer than %d", len(v.rows), before)
	}
	if v.rows[v.cursor].kind != rowFileHeader {
		t.Error("cursor must stay on the file header after folding")
	}
	v.toggleFold()
	if len(v.rows) != before {
		t.Errorf("rows = %d after unfold, want %d", len(v.rows), before)
	}
}

func TestThreadRangeNormalizesInvertedSpan(t *testing.T) {
	// GitHub can report an outdated thread with the ends swapped; rendering it
	// verbatim produces "468-416".
	lo, hi, ok := threadRange(pr.ReviewThread{StartLine: 468, Line: 416})
	if !ok {
		t.Fatal("threadRange: not ok")
	}
	if lo != 416 || hi != 468 {
		t.Errorf("got %d-%d, want 416-468", lo, hi)
	}
}

func TestCommentPreviewStripsMarkup(t *testing.T) {
	body := "**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-red)</sub></sub> " +
		"Keep the guard active**\n\nSee [AGENTS.md](https://example.com/AGENTS.md)."
	got := commentPreview(body)
	for _, unwanted := range []string{"![", "shields.io", "<sub>", "**", "\n"} {
		if contains(got, unwanted) {
			t.Errorf("preview still contains %q: %s", unwanted, got)
		}
	}
	if !contains(got, "Keep the guard active") {
		t.Errorf("preview dropped the finding text: %s", got)
	}
	if !contains(got, "AGENTS.md") {
		t.Errorf("preview should keep link text: %s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

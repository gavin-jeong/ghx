package tui

import (
	"strings"
	"testing"

	"github.com/keyolk/ghx/internal/pr"
)

// A review comment the reviewer cannot see is worse than one shown out of
// place: the author still has to answer it. These tests pin down that every
// thread the PR carries ends up somewhere in the diff view.

const threadsDiff = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -10,2 +10,3 @@
 keep
+added
`

func threadViewWith(t *testing.T, threads []pr.ReviewThread) *diffView {
	t.Helper()
	v := newDiffView()
	if err := v.setContent(threadsDiff, threads); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	return v
}

func renderedThreadIDs(v *diffView) map[string]bool {
	out := map[string]bool{}
	for _, r := range v.rows {
		if r.kind == rowThread {
			out[r.threadID] = true
		}
	}
	return out
}

// An outdated thread reports a line the current diff no longer contains. It must
// still be listed, flagged as out of place, rather than dropped.
func TestOutdatedThreadStillListed(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{
		{ID: "anchored", Path: "app.go", Line: 11, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "on a visible line"}}},
		{ID: "outdated", Path: "app.go", Line: 999, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "anchor is gone"}}},
	})
	seen := renderedThreadIDs(v)
	if !seen["anchored"] {
		t.Error("anchored thread missing")
	}
	if !seen["outdated"] {
		t.Fatal("outdated thread was dropped from the view")
	}
	out := v.render(140, 40)
	if !contains(out, "anchor is gone") {
		t.Error("the outdated comment's text should be readable")
	}
	// Without a note it would read as a comment on whatever line precedes it.
	if !contains(out, "not in this diff") {
		t.Errorf("an out-of-place thread must say so: %s", out)
	}
}

// A force-push can leave threads on files the new diff does not touch at all.
// Those have no file header to sit under and were previously invisible.
func TestThreadsOnFilesAbsentFromDiffAreGrouped(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{
		{ID: "elsewhere", Path: "removed.go", Line: 5, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "comment on a file not in the diff"}}},
	})
	if !renderedThreadIDs(v)["elsewhere"] {
		t.Fatal("thread on an absent file was dropped")
	}
	out := v.render(140, 40)
	if !contains(out, "files not in this diff") {
		t.Errorf("absent-file threads need a heading: %s", out)
	}
	if !contains(out, "comment on a file not in the diff") {
		t.Error("the comment body should be readable")
	}
}

// Replies carry the substance of a discussion. Collapsed by default so a long
// bot thread does not bury the code, but they must be reachable.
func TestRepliesHiddenUntilExpanded(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{
			{Body: "opening point", Author: pr.User{Login: "alice"}},
			{Body: "the actual answer", Author: pr.User{Login: "bob"}},
			{Body: "one more thing", Author: pr.User{Login: "carol"}},
		},
	}})

	collapsed := v.render(140, 40)
	if !contains(collapsed, "opening point") {
		t.Error("the opening comment must always show")
	}
	if contains(collapsed, "the actual answer") {
		t.Error("replies should start collapsed")
	}
	if !contains(collapsed, "2 replies") {
		t.Errorf("a collapsed thread should say how many replies it hides: %s", collapsed)
	}

	// Park the cursor on the thread and expand it.
	for i, r := range v.rows {
		if r.kind == rowThread {
			v.cursor = i
			break
		}
	}
	if !v.toggleThread() {
		t.Fatal("toggleThread did nothing on a thread row")
	}
	expanded := v.render(140, 40)
	for _, want := range []string{"opening point", "the actual answer", "one more thing"} {
		if !contains(expanded, want) {
			t.Errorf("expanded thread is missing %q", want)
		}
	}
	if !contains(expanded, "bob") || !contains(expanded, "carol") {
		t.Error("each reply should name its author")
	}
}

// Expanding must not move the cursor off the thread: the next keystroke is
// usually a reply, and a shifted cursor would target something else.
func TestToggleThreadKeepsCursorOnThread(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{
			{Body: "one", Author: pr.User{Login: "a"}},
			{Body: "two", Author: pr.User{Login: "b"}},
		},
	}})
	for i, r := range v.rows {
		if r.kind == rowThread {
			v.cursor = i
			break
		}
	}
	v.toggleThread()
	r := v.rows[v.cursor]
	if r.kind != rowThread || r.threadID != "T" || r.commentIdx != 0 {
		t.Errorf("cursor landed on kind=%v id=%q commentIdx=%d, want the thread's opening row",
			r.kind, r.threadID, r.commentIdx)
	}

	v.toggleThread() // collapse again
	r = v.rows[v.cursor]
	if r.kind != rowThread || r.threadID != "T" {
		t.Error("collapsing should also leave the cursor on the thread")
	}
}

// A single-comment thread has nothing to expand; the fold marker would be a
// promise the view cannot keep.
func TestSingleCommentThreadHasNoFoldMarker(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "just one", Author: pr.User{Login: "a"}}},
	}})
	if v.threadHasReplies() {
		// cursor is not on the thread yet, so this should be false anyway
		t.Log("threadHasReplies is true with the cursor off-thread")
	}
	for i, r := range v.rows {
		if r.kind == rowThread {
			v.cursor = i
			break
		}
	}
	if v.threadHasReplies() {
		t.Error("a one-comment thread reports replies it does not have")
	}
	row := v.rows[v.cursor].text
	if strings.HasPrefix(strings.TrimSpace(row), iconFoldClosed) {
		t.Errorf("no fold marker belongs on a single-comment thread: %q", row)
	}
}

// Every thread must be accounted for, whatever its anchor. This is the property
// that matters: the count in the Comments tab and the diff view should agree.
func TestEveryThreadIsRenderedSomewhere(t *testing.T) {
	threads := []pr.ReviewThread{
		{ID: "a", Path: "app.go", Line: 11, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "anchored right"}}},
		{ID: "b", Path: "app.go", Line: 10, DiffSide: "LEFT",
			Comments: []pr.ThreadComment{{Body: "anchored left"}}},
		{ID: "c", Path: "app.go", Line: 500, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "outdated"}}},
		{ID: "d", Path: "other.go", Line: 1, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "another file"}}},
		{ID: "e", Path: "app.go", Line: 0, OriginalLine: 11, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "line 0, falls back to original"}}},
	}
	v := threadViewWith(t, threads)
	seen := renderedThreadIDs(v)
	for _, want := range []string{"a", "b", "c", "d", "e"} {
		if !seen[want] {
			t.Errorf("thread %q is not rendered anywhere", want)
		}
	}
	if len(seen) != len(threads) {
		t.Errorf("rendered %d distinct threads, want %d", len(seen), len(threads))
	}
}

// Threads must survive the side-by-side layout too — the whole point is
// reviewing with the discussion in view.
func TestThreadsVisibleInSideBySide(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{
		{ID: "anchored", Path: "app.go", Line: 11, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "visible in both layouts"}}},
		{ID: "outdated", Path: "app.go", Line: 999, DiffSide: "RIGHT",
			Comments: []pr.ThreadComment{{Body: "outdated but listed"}}},
	})
	v.sideBySide = true
	out := v.renderSideBySide(160, 40)
	if !contains(out, "visible in both layouts") {
		t.Error("anchored thread missing from side-by-side")
	}
	if !contains(out, "outdated but listed") {
		t.Error("outdated thread missing from side-by-side")
	}
}

// Folding a file hides its hunks; its threads should go with them rather than
// dangling under a collapsed header.
func TestFoldingFileHidesItsThreads(t *testing.T) {
	v := threadViewWith(t, []pr.ReviewThread{{
		ID: "T", Path: "app.go", Line: 11, DiffSide: "RIGHT",
		Comments: []pr.ThreadComment{{Body: "inside app.go"}},
	}})
	if !renderedThreadIDs(v)["T"] {
		t.Fatal("thread should be visible before folding")
	}
	v.cursor = 0 // file header
	v.toggleFold()
	if renderedThreadIDs(v)["T"] {
		t.Error("a folded file should not leave its threads on screen")
	}
	v.toggleFold()
	if !renderedThreadIDs(v)["T"] {
		t.Error("unfolding should bring the thread back")
	}
}

// Jumping from the Comments tab has to bring the target on screen in whichever
// layout is active. The two layouts keep separate scroll offsets, so updating
// only the cursor left side-by-side showing wherever it happened to be — the
// jump looked like it did nothing.
func TestJumpToScrollsBothLayouts(t *testing.T) {
	// A diff long enough that the target starts well off screen.
	var b strings.Builder
	b.WriteString("diff --git a/app.go b/app.go\n--- a/app.go\n+++ b/app.go\n@@ -1,60 +1,60 @@\n")
	for i := 0; i < 60; i++ {
		b.WriteString(" context line\n")
	}
	const height = 12

	for _, side := range []bool{false, true} {
		name := "unified"
		if side {
			name = "side-by-side"
		}
		t.Run(name, func(t *testing.T) {
			v := newDiffView()
			if err := v.setContent(b.String(), nil); err != nil {
				t.Fatalf("setContent: %v", err)
			}
			v.sideBySide = side

			// Scroll to the top, then jump to a line far down.
			v.cursor, v.offset, v.sideOffset = 0, 0, 0
			target := 0
			for i, r := range v.rows {
				if r.kind == rowDiffLine && r.side == "RIGHT" && r.anchorLine == 50 {
					target = i
					break
				}
			}
			if target == 0 {
				t.Fatal("could not find RIGHT:50 in the diff")
			}
			v.jumpTo("app.go", 50)
			if v.cursor != target {
				t.Fatalf("cursor = %d, want %d", v.cursor, target)
			}

			// Rendering must place the cursor inside the visible window.
			if side {
				v.renderSideBySide(160, height)
				pairs := v.sideRows()
				cp := v.cursorPairIndex(pairs)
				if cp < v.sideOffset || cp >= v.sideOffset+height {
					t.Errorf("cursor pair %d outside window [%d,%d) — the jump did not scroll",
						cp, v.sideOffset, v.sideOffset+height)
				}
			} else {
				v.render(120, height)
				if v.cursor < v.offset || v.cursor >= v.offset+height {
					t.Errorf("cursor %d outside window [%d,%d) — the jump did not scroll",
						v.cursor, v.offset, v.offset+height)
				}
			}
		})
	}
}

// Switching layout after a jump must not lose the position either: the offset
// for the newly active layout is stale and has to be recomputed.
func TestLayoutSwitchAfterJumpKeepsCursorVisible(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/app.go b/app.go\n--- a/app.go\n+++ b/app.go\n@@ -1,40 +1,40 @@\n")
	for i := 0; i < 40; i++ {
		b.WriteString(" context line\n")
	}
	v := newDiffView()
	if err := v.setContent(b.String(), nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	const height = 10

	v.jumpTo("app.go", 30)
	v.render(120, height) // settle the unified offset
	if v.cursor < v.offset || v.cursor >= v.offset+height {
		t.Fatalf("unified: cursor %d outside [%d,%d)", v.cursor, v.offset, v.offset+height)
	}

	v.sideBySide = true
	v.renderSideBySide(160, height)
	pairs := v.sideRows()
	cp := v.cursorPairIndex(pairs)
	if cp < v.sideOffset || cp >= v.sideOffset+height {
		t.Errorf("after switching layout the cursor pair %d is outside [%d,%d)",
			cp, v.sideOffset, v.sideOffset+height)
	}
}

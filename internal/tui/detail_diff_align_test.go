package tui

import (
	"strings"
	"testing"
)

// A terminal advances a tab to the next tab stop, while width calculations count
// it as a single cell. Code is full of tabs, so left unexpanded a line occupies
// more columns than the layout budgeted — the divider lands in a different place
// on every row and the two sides drift apart.

// tabbedDiff has the leading tabs and nested indentation real Go code has.
const tabbedDiff = "diff --git a/x.go b/x.go\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -193,7 +194,6 @@\n" +
	" \treturn nil\n" +
	"-\tlegacyKeys := make(map[string]string)\n" +
	" \tpaginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{\n" +
	" \t\tBucket: aws.String(historyS3Bucket),\n" +
	" \t\t\tPrefix: aws.String(skillsS3KeyPrefix),\n"

func TestExpandTabs(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		startCol int
		want     string
	}{
		{"leading tab at column 0", "\tx", 0, "        x"},
		// A tab's width depends on where it lands: from column 7 it advances one
		// cell to reach column 8, not eight cells.
		{"tab from column 7", "\tx", 7, " x"},
		{"tab from column 8", "\tx", 8, "        x"},
		{"two tabs", "\t\tx", 0, "                x"},
		{"tab after text", "ab\tx", 0, "ab      x"},
		{"no tabs is untouched", "plain text", 0, "plain text"},
		{"empty", "", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expandTabs(c.in, c.startCol); got != c.want {
				t.Errorf("expandTabs(%q, %d) = %q, want %q", c.in, c.startCol, got, c.want)
			}
		})
	}
}

// A CJK rune advances two cells, so the following tab has to account for that or
// the expansion would be one stop off.
func TestExpandTabsAccountsForWideRunes(t *testing.T) {
	// "한" is two cells; from column 0 it ends at 2, so the tab adds 6.
	got := expandTabs("한\tx", 0)
	want := "한" + strings.Repeat(" ", 6) + "x"
	if got != want {
		t.Errorf("expandTabs = %q, want %q", got, want)
	}
	if w := lipglossWidth(got); w != 9 {
		t.Errorf("expanded width = %d, want 9 (2 + 6 + 1)", w)
	}
}

// The whole point: every rendered row must be exactly the requested width, and
// the divider must sit in the same column on all of them.
func TestSideBySideRowsStayAlignedWithTabs(t *testing.T) {
	v := newDiffView()
	if err := v.setContent(tabbedDiff, nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	v.sideBySide = true

	for _, width := range []int{100, 170, 200} {
		t.Run(itoa(width), func(t *testing.T) {
			out := v.renderSideBySide(width, 12)
			dividers := map[int]int{}
			for i, line := range strings.Split(out, "\n") {
				if got := lipglossWidth(line); got > width {
					t.Errorf("row %d renders %d cells, want at most %d: %q",
						i, got, width, stripANSISeqs(line))
				}
				plain := stripANSISeqs(line)
				if idx := strings.Index(plain, "│"); idx >= 0 {
					dividers[idx]++
				}
			}
			if len(dividers) > 1 {
				t.Errorf("the divider sits in %d different columns %v — the columns are misaligned",
					len(dividers), dividers)
			}
		})
	}
}

// The unified layout has the same trap: a tabbed line must not exceed the pane.
func TestUnifiedRowsFitWidthWithTabs(t *testing.T) {
	v := newDiffView()
	if err := v.setContent(tabbedDiff, nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	for _, width := range []int{60, 100, 160} {
		for i, line := range strings.Split(v.render(width, 12), "\n") {
			if got := lipglossWidth(line); got > width {
				t.Errorf("width %d: row %d renders %d cells: %q",
					width, i, got, stripANSISeqs(line))
			}
		}
	}
}

// Indentation must survive: expanding tabs is about measuring correctly, not
// about flattening the code's structure.
func TestTabExpansionPreservesIndentationDepth(t *testing.T) {
	v := newDiffView()
	if err := v.setContent(tabbedDiff, nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	out := stripANSISeqs(v.render(200, 12))

	indentOf := func(needle string) int {
		for _, line := range strings.Split(out, "\n") {
			if i := strings.Index(line, needle); i >= 0 {
				// Count spaces immediately before the token.
				n := 0
				for j := i - 1; j >= 0 && line[j] == ' '; j-- {
					n++
				}
				return n
			}
		}
		return -1
	}

	shallow := indentOf("return nil")
	deeper := indentOf("Bucket:")
	deepest := indentOf("Prefix:")
	if shallow < 0 || deeper < 0 || deepest < 0 {
		t.Fatalf("lines not found (%d, %d, %d)", shallow, deeper, deepest)
	}
	if !(shallow < deeper && deeper < deepest) {
		t.Errorf("indentation collapsed: %d, %d, %d cells — nesting should increase",
			shallow, deeper, deepest)
	}
}

// The selected row goes through a different render path (plain text under a
// theme), which must expand tabs too or the highlight would be the wrong length.
func TestSelectedRowWithTabsFitsWidth(t *testing.T) {
	forceColor(t)
	v := newDiffView()
	if err := v.setContent(tabbedDiff, nil); err != nil {
		t.Fatalf("setContent: %v", err)
	}
	// Park the cursor on a deeply indented line.
	for i, r := range v.rows {
		if r.kind == rowDiffLine && strings.Contains(r.line.Content, "Prefix:") {
			v.cursor = i
			break
		}
	}
	const width = 120
	for _, line := range strings.Split(v.render(width, 12), "\n") {
		if got := lipglossWidth(line); got > width {
			t.Errorf("selected row renders %d cells, want at most %d: %q",
				got, width, stripANSISeqs(line))
		}
	}
}

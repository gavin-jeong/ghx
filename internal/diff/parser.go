// Package diff parses unified diff output (from `gh pr diff`) into per-file
// hunks with LEFT/RIGHT line numbers. Correctness here determines whether
// inline review comments land on the right lines, so the line-number mapping
// is the part to scrutinize: additions exist only on RIGHT, deletions only on
// LEFT, context on both.
package diff

import (
	"strconv"
	"strings"

	"github.com/keyolk/ghx/internal/pr"
)

// Parse converts a unified diff into per-file hunks. Unparseable trailing
// content is ignored rather than failing the whole diff — a malformed tail
// shouldn't hide the files that did parse.
func Parse(unified string) ([]pr.DiffFile, error) {
	var files []pr.DiffFile
	var cur *pr.DiffFile
	var hunk *pr.DiffHunk
	// Running line counters for the active hunk.
	var oldNo, newNo int

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	lines := strings.Split(unified, "\n")
	// strings.Split yields a trailing "" for the diff's final newline. That
	// empty string is not a diff line — counting it as context would add a
	// phantom line and shift every subsequent line number.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			path, oldPath := parseDiffGit(line)
			cur = &pr.DiffFile{Path: path, OldPath: oldPath}

		case cur == nil:
			// Content before the first `diff --git` header — skip.
			continue

		case strings.HasPrefix(line, "rename from "):
			cur.OldPath = strings.TrimPrefix(line, "rename from ")

		case strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimPrefix(line, "rename to ")

		case strings.HasPrefix(line, "--- "):
			// Old-file header. "--- /dev/null" means the file was added.
			continue

		case strings.HasPrefix(line, "+++ "):
			// New-file header; authoritative path for the RIGHT side.
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				cur.Path = stripABPrefix(p)
			}

		case strings.HasPrefix(line, "@@"):
			flushHunk()
			h, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			hunk = &h
			oldNo = h.OldStart
			newNo = h.NewStart

		case hunk == nil:
			// index/mode/binary lines outside a hunk — skip.
			continue

		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" — metadata about the previous line.
			continue

		case strings.HasPrefix(line, "+"):
			hunk.Lines = append(hunk.Lines, pr.DiffLine{
				Kind:      pr.DiffLineAddition,
				Content:   line[1:],
				NewLineNo: newNo,
			})
			newNo++
			cur.Additions++

		case strings.HasPrefix(line, "-"):
			hunk.Lines = append(hunk.Lines, pr.DiffLine{
				Kind:      pr.DiffLineDeletion,
				Content:   line[1:],
				OldLineNo: oldNo,
			})
			oldNo++
			cur.Deletions++

		case strings.HasPrefix(line, " "):
			hunk.Lines = append(hunk.Lines, pr.DiffLine{
				Kind:      pr.DiffLineContext,
				Content:   line[1:],
				OldLineNo: oldNo,
				NewLineNo: newNo,
			})
			oldNo++
			newNo++

		case line == "":
			// A bare empty line inside a hunk is a context line whose single
			// leading space was stripped by a tool or transport. Accept it only
			// while the hunk still expects lines — otherwise this is the empty
			// string after the diff's trailing newline (or the blank separator
			// before the next `diff --git`), and counting it would shift every
			// subsequent line number and misplace inline comments.
			if hunkHasRoom(hunk, oldNo, newNo) {
				hunk.Lines = append(hunk.Lines, pr.DiffLine{
					Kind:      pr.DiffLineContext,
					Content:   "",
					OldLineNo: oldNo,
					NewLineNo: newNo,
				})
				oldNo++
				newNo++
			} else {
				flushHunk()
			}

		default:
			// Unknown line inside a hunk — end the hunk rather than guessing.
			flushHunk()
		}
	}
	flushFile()
	return files, nil
}

// hunkHasRoom reports whether the hunk's declared ranges still expect more
// lines. Used to tell a genuine (space-stripped) context line apart from the
// empty string produced by the diff's trailing newline.
func hunkHasRoom(h *pr.DiffHunk, oldNo, newNo int) bool {
	if h == nil {
		return false
	}
	oldRoom := oldNo < h.OldStart+h.OldCount
	newRoom := newNo < h.NewStart+h.NewCount
	return oldRoom && newRoom
}

// parseDiffGit extracts the new and old paths from a `diff --git a/x b/y` line.
func parseDiffGit(line string) (newPath, oldPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	// Paths may contain spaces; the common case is `a/<p> b/<p>`. Split on
	// " b/" which reliably separates the two halves for git-generated diffs.
	if idx := strings.Index(rest, " b/"); idx >= 0 {
		a := stripABPrefix(rest[:idx])
		b := stripABPrefix(rest[idx+1:])
		if a == b {
			return b, ""
		}
		return b, a
	}
	fields := strings.Fields(rest)
	if len(fields) == 2 {
		a := stripABPrefix(fields[0])
		b := stripABPrefix(fields[1])
		if a == b {
			return b, ""
		}
		return b, a
	}
	return stripABPrefix(rest), ""
}

// stripABPrefix removes a leading "a/" or "b/" and any trailing tab metadata.
func stripABPrefix(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// parseHunkHeader parses "@@ -oldStart,oldCount +newStart,newCount @@ ctx".
// Counts are optional and default to 1 (git omits ",1").
func parseHunkHeader(line string) (pr.DiffHunk, bool) {
	var h pr.DiffHunk
	// Isolate the range section between the leading and trailing "@@".
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return h, false
	}
	ranges := strings.Fields(rest[:end])
	if len(ranges) < 2 {
		return h, false
	}
	oldPart := strings.TrimPrefix(ranges[0], "-")
	newPart := strings.TrimPrefix(ranges[1], "+")
	os, oc, ok1 := splitRange(oldPart)
	ns, nc, ok2 := splitRange(newPart)
	if !ok1 || !ok2 {
		return h, false
	}
	h.OldStart, h.OldCount = os, oc
	h.NewStart, h.NewCount = ns, nc
	return h, true
}

// splitRange parses "start,count" or "start" (count defaults to 1).
func splitRange(s string) (start, count int, ok bool) {
	parts := strings.SplitN(s, ",", 2)
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	start = n
	count = 1
	if len(parts) == 2 {
		c, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
		count = c
	}
	return start, count, true
}

// CommentTarget returns the (side, line) an inline comment should attach to for
// a given diff line. Additions and context anchor to RIGHT; deletions to LEFT.
// Returns ok=false for header lines, which cannot host a comment.
func CommentTarget(l pr.DiffLine) (side string, line int, ok bool) {
	switch l.Kind {
	case pr.DiffLineAddition:
		return "RIGHT", l.NewLineNo, true
	case pr.DiffLineDeletion:
		return "LEFT", l.OldLineNo, true
	case pr.DiffLineContext:
		return "RIGHT", l.NewLineNo, true
	}
	return "", 0, false
}

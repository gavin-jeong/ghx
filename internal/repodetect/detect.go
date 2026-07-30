// Package repodetect works out which GitHub repository the user is currently
// looking at, so the PR list can lead with that repo instead of a generic queue.
//
// Two signals are used, in order of confidence: the working directory's git
// remote, and the current tmux window's pane paths. The directory is the
// stronger signal — it is where the user actually is — but ghx is often launched
// from elsewhere while the work sits in another pane of the same window.
package repodetect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result is a detected repository and where the hint came from.
type Result struct {
	Slug   string // "owner/name"
	Source string // "cwd" | "tmux" | ""
	Path   string // the directory the slug was resolved from
}

// Found reports whether anything was detected.
func (r Result) Found() bool { return r.Slug != "" }

// timeout bounds the whole detection: it runs at startup, and a slow git call on
// a network filesystem must not delay the first frame.
const timeout = 2 * time.Second

// Detect returns the repository to prioritise, or an empty Result.
//
// startDir is normally the process's working directory; it is a parameter so
// tests can drive detection without chdir.
func Detect(ctx context.Context, startDir string) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if slug, root := slugFromDir(ctx, startDir); slug != "" {
		return Result{Slug: slug, Source: "cwd", Path: root}
	}

	// The directory is not a checkout. The tmux window can still say what the
	// user is working on — ghx is often launched in a scratch pane beside the
	// code — but only the *active* pane is trusted. Scanning every pane would
	// pick up whatever unrelated repo happens to be open elsewhere in the
	// window, which makes the leading tab unpredictable.
	if dir := tmuxActivePanePath(ctx); dir != "" && !sameDir(dir, startDir) {
		if slug, root := slugFromDir(ctx, dir); slug != "" {
			return Result{Slug: slug, Source: "tmux", Path: root}
		}
	}
	return Result{}
}

// sameDir reports whether two paths name the same directory, so a fallback does
// not re-test the directory that already failed.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// slugFromDir resolves a directory to "owner/name" via its git remote, and
// returns the repository root it belongs to.
//
// A linked worktree resolves to the same slug as its main checkout, which is
// what makes `.worktree/<branch>` directories behave as the repo they came from.
func slugFromDir(ctx context.Context, dir string) (slug, root string) {
	if dir == "" {
		return "", ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", ""
	}
	root = strings.TrimSpace(gitOut(ctx, dir, "rev-parse", "--show-toplevel"))
	if root == "" {
		return "", ""
	}
	// Try the conventional remote names before falling back to whatever exists,
	// so a fork with both `origin` and `upstream` prefers the one being pushed to.
	for _, remote := range []string{"origin", "upstream"} {
		if url := strings.TrimSpace(gitOut(ctx, dir, "remote", "get-url", remote)); url != "" {
			if s := SlugFromURL(url); s != "" {
				return s, root
			}
		}
	}
	for _, remote := range strings.Fields(gitOut(ctx, dir, "remote")) {
		if url := strings.TrimSpace(gitOut(ctx, dir, "remote", "get-url", remote)); url != "" {
			if s := SlugFromURL(url); s != "" {
				return s, root
			}
		}
	}
	return "", ""
}

// SlugFromURL extracts "owner/name" from a git remote URL. It handles the https
// and ssh forms git writes, and returns "" for anything that is not GitHub —
// a GitLab remote must not be offered to the GitHub API.
func SlugFromURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimSuffix(u, ".git")

	switch {
	case strings.HasPrefix(u, "git@"):
		// git@github.com:owner/name
		host, path, ok := strings.Cut(strings.TrimPrefix(u, "git@"), ":")
		if !ok || !isGitHubHost(host) {
			return ""
		}
		u = path
	case strings.Contains(u, "://"):
		// https://github.com/owner/name, ssh://git@github.com/owner/name
		_, rest, _ := strings.Cut(u, "://")
		// Drop any user@ prefix on the host.
		if _, after, ok := strings.Cut(rest, "@"); ok {
			rest = after
		}
		host, path, ok := strings.Cut(rest, "/")
		if !ok || !isGitHubHost(host) {
			return ""
		}
		u = path
	default:
		return ""
	}

	parts := strings.Split(strings.Trim(u, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	// Deeper paths (GitHub Enterprise subgroups do not exist, but be tolerant of
	// trailing segments) still identify the repo by its first two components.
	return parts[0] + "/" + parts[1]
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i] // strip a port
	}
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}

// tmuxActivePanePath returns the current window's active pane directory, or "".
//
// Only the active pane is consulted. It is the one thing in the window that
// reliably says what the user is doing; the other panes hold logs, shells, and
// unrelated checkouts, and letting any of them win would make the leading tab
// depend on pane order.
func tmuxActivePanePath(ctx context.Context) string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out := run(ctx, "", "tmux", "display-message", "-p", "#{pane_current_path}")
	return strings.TrimSpace(out)
}

func gitOut(ctx context.Context, dir string, args ...string) string {
	return run(ctx, dir, "git", args...)
}

// run executes a command and returns its stdout, or "" on any failure. Detection
// is best-effort: a missing git, a directory that is not a repo, and a tmux that
// is not running are all ordinary outcomes, not errors to report.
func run(ctx context.Context, dir, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

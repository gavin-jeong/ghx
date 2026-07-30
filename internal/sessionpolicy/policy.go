// Package sessionpolicy reports whether merging is permitted. Merging a PR is
// irreversible, so ghx treats it as blocked unless something explicitly says
// otherwise: the default answer is always "no".
package sessionpolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// MergeAllowed reports whether `gh pr merge` may run. It fails closed: any
// error, missing file, or unrecognized value yields false.
//
// Two sources are consulted, in order:
//  1. GHX_ALLOW_MERGE=1 in the environment (explicit, per-invocation opt-in).
//  2. ~/.claude/state/session_policy — the Claude Code session-policy state,
//     where a gh_merge entry of "allow" unlocks merging.
func MergeAllowed() bool {
	if v := strings.TrimSpace(os.Getenv("GHX_ALLOW_MERGE")); v == "1" || strings.EqualFold(v, "allow") {
		return true
	}
	return policyFileAllows()
}

// policyFileAllows reads the session-policy state file and looks for a
// gh_merge=allow entry. The file's exact schema has varied, so this accepts
// either a flat {"gh_merge":"allow"} map or a nested
// {"<session-id>":{"gh_merge":"allow"}} shape, and ignores anything else.
func policyFileAllows() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "state", "session_policy"))
	if err != nil {
		return false
	}

	// Flat shape first.
	var flat map[string]string
	if err := json.Unmarshal(data, &flat); err == nil {
		return isAllow(flat["gh_merge"])
	}

	// Nested per-session shape.
	var nested map[string]map[string]string
	if err := json.Unmarshal(data, &nested); err == nil {
		session := os.Getenv("CLAUDE_CODE_SESSION_ID")
		if session != "" {
			if entry, ok := nested[session]; ok {
				return isAllow(entry["gh_merge"])
			}
			// A policy exists for other sessions but not this one — stay locked.
			return false
		}
		// No session id to match against: only unlock when every entry allows,
		// so a single blocking session can't be bypassed.
		if len(nested) == 0 {
			return false
		}
		for _, entry := range nested {
			if !isAllow(entry["gh_merge"]) {
				return false
			}
		}
		return true
	}
	return false
}

func isAllow(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "allow")
}

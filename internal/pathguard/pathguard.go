// Package pathguard provides separator-aware path-containment checks and
// symlink-aware canonicalization shared by the MCP sandbox (per-call
// tool-input validation) and the CLI home guard (startup confinement of
// the monitored directory to $HOME).
//
// Both inputs to Under are expected to be canonical (see Canonical) so
// the prefix comparison is meaningful. The logic here was extracted from
// the original internal/mcpserver/sandbox.go so the two features can't
// drift in how they decide "is this path inside that directory?".
package pathguard

import (
	"path/filepath"
	"strings"
)

// Under is a separator-aware "is p inside root?" prefix check. Returns
// true when p == root (the root itself counts as inside) or when p
// starts with root + the path separator — so "/home/foo-bar" doesn't
// sneak through when root is "/home/foo". Both inputs are expected to be
// canonical (Canonical) before being passed.
func Under(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// ResolveDeepest walks up abs until filepath.EvalSymlinks succeeds, then
// reattaches the non-existent suffix. This canonicalises symlinked
// ancestors (e.g. /tmp → /private/tmp on macOS) for paths that don't
// fully exist yet — important so containment checks see the same
// canonical form for both existing and not-yet-existing inputs. Returns
// abs unchanged when no ancestor resolves.
func ResolveDeepest(abs string) string {
	suffix := ""
	cur := abs
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if suffix == "" {
				return filepath.Clean(r)
			}
			return filepath.Clean(filepath.Join(r, suffix))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the root without finding a resolvable ancestor.
			return abs
		}
		base := filepath.Base(cur)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		cur = parent
	}
}

// Canonical resolves path to an absolute, symlink-aware canonical form:
// filepath.Abs → filepath.Clean → ResolveDeepest. Falls back to a
// lexical filepath.Clean when Abs fails (e.g. the cwd is unavailable).
// Use it on BOTH sides of an Under check so a symlinked $HOME or a macOS
// /tmp quirk doesn't cause a false mismatch.
func Canonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return ResolveDeepest(filepath.Clean(abs))
}

package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// errSandboxFollowSymlinksUnsupported is returned when a walk tool is
// called with follow_symlinks=true while the sandbox is active. The
// walker doesn't yet enforce the sandbox per-entry, so following
// symlinks could escape — defence-in-depth: reject up-front rather
// than ship a leak. Plumbing the sandbox into search.Options is the
// long-term fix; see the v1 plan's "out of scope".
var errSandboxFollowSymlinksUnsupported = errors.New("sandbox active: follow_symlinks=true is unsupported in this release (the walker does not yet enforce the sandbox per-entry)")

// WithSandbox configures the server to reject every path-accepting tool
// input that resolves outside the given roots. Empty / nil = unrestricted
// (today's behaviour). Roots are canonicalised at construction time:
// each is filepath.Abs + filepath.Clean. Roots that fail Abs are
// silently dropped — operators get the stderr startup line to verify
// what landed.
//
// Operators pass roots from --sandbox / --sandbox-dir on the mcp
// subcommand; agents see a clear MCP tool error (IsError=true) when
// they try to access a path outside the canonical roots.
func WithSandbox(roots []string) Option {
	return func(h *handlers) {
		if len(roots) == 0 {
			return
		}
		canonical := make([]string, 0, len(roots))
		for _, r := range roots {
			if r == "" {
				continue
			}
			abs, err := filepath.Abs(r)
			if err != nil {
				continue
			}
			abs = filepath.Clean(abs)
			// EvalSymlinks the root so canonical comparison is
			// symmetric: validatePath EvalSymlinks the input, so
			// roots that traverse a symlink (e.g. /tmp → /private/tmp
			// on macOS) must be resolved the same way. Non-existent
			// roots fall back to the lexical form — operators can
			// pre-configure paths that get populated later.
			if r2, err := filepath.EvalSymlinks(abs); err == nil {
				abs = filepath.Clean(r2)
			}
			canonical = append(canonical, abs)
		}
		h.sandbox = canonical
	}
}

// validatePath returns p (after the caller's expandHomeDir step) if it
// resolves under any configured sandbox root, or an error otherwise.
// Empty sandbox = pass-through (sandbox is opt-in).
//
// Validation order:
//  1. filepath.Abs canonicalises relative paths against the server's cwd.
//  2. filepath.EvalSymlinks, when the target exists, resolves symlinks
//     so a "ln -s /etc/passwd <root>/sneaky" inside the sandbox is
//     caught. Non-existent paths fall back to the lexical Abs+Clean —
//     the actual filesystem op will produce its own clear error.
//  3. Separator-aware prefix check against every canonical root.
//     "/home/foo" doesn't match a root of "/home/foo-bar" (and vice
//     versa) thanks to the trailing-separator-or-exact comparison.
//
// On success returns the lexical absolute path (not the EvalSymlinks
// resolution), so callers that pass the result straight into the
// walker / file open see the same path the agent supplied — keeps
// downstream output stable for symlink-aware callers.
func (h *handlers) validatePath(p string) (string, error) {
	if len(h.sandbox) == 0 {
		return p, nil
	}
	if p == "" {
		// Empty inputs typically mean "use the tool's default" (cwd)
		// — pass through so the handler's existing default logic
		// (which may then call validatePath again with the resolved
		// cwd) is the chokepoint.
		return p, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve %q: %w", p, err)
	}
	abs = filepath.Clean(abs)
	resolved := resolveDeepest(abs)
	for _, root := range h.sandbox {
		if pathUnder(resolved, root) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("sandbox: %q is outside the allowed roots %v", p, h.sandbox)
}

// resolveDeepest walks up p until filepath.EvalSymlinks succeeds, then
// reattaches the non-existent suffix. This canonicalises symlinked
// ancestors (e.g. /tmp → /private/tmp on macOS) for paths that don't
// fully exist yet — important so the sandbox check sees the same
// canonical form for both existing and not-yet-existing inputs.
// Returns abs unchanged when no ancestor resolves.
func resolveDeepest(abs string) string {
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

// validatePaths is the slice variant for tools that take `dirs` /
// `tree_a` + `tree_b` style multiple roots. Returns an error on the
// first violation so the agent sees a deterministic failure mode (not
// partial-success with a silently-dropped entry).
func (h *handlers) validatePaths(ps []string) ([]string, error) {
	if len(h.sandbox) == 0 || len(ps) == 0 {
		return ps, nil
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		v, err := h.validatePath(p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// checkFollowSymlinks rejects a tool call when follow_symlinks=true and
// the sandbox is active. See errSandboxFollowSymlinksUnsupported.
// Returns nil when either the sandbox is off, or follow_symlinks is
// false — the common cases.
func (h *handlers) checkFollowSymlinks(follow bool) error {
	if len(h.sandbox) > 0 && follow {
		return errSandboxFollowSymlinksUnsupported
	}
	return nil
}

// pathUnder is a separator-aware "is p inside root?" prefix check.
// Returns true when p == root (the root itself is a valid input) or
// when p starts with root + the path separator (so "/home/foo-bar"
// doesn't sneak through when root is "/home/foo"). Both inputs are
// expected to be canonical (filepath.Clean + Abs) before being passed.
func pathUnder(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

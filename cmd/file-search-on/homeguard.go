package main

import (
	"fmt"
	"os"

	"github.com/richardwooding/file-search-on/internal/pathguard"
)

// ensureUnderHome is the startup safety guard for the long-running
// subcommands (mcp, watch): it refuses to proceed unless every monitored
// directory resolves inside the user's home directory. This stops an
// agent-driven server or a background watcher from being pointed at
// system paths or an entire volume by an errant cwd / --warm-dir / -d.
//
// Semantics:
//   - allowOutside short-circuits the guard (explicit operator opt-out
//     via --allow-outside-home), printing a one-line stderr notice.
//   - Fail-closed: an unresolvable $HOME is itself an error — the guard
//     can't confine what it can't locate, so it refuses rather than
//     silently passing. Container / CI runners with no $HOME must set
//     HOME or pass --allow-outside-home.
//   - Both the candidate dir and $HOME are canonicalised the same way
//     (pathguard.Canonical: abs + clean + symlink-resolve) so a
//     symlinked home or a macOS /tmp→/private/tmp quirk never causes a
//     false reject. $HOME itself counts as inside (inclusive boundary).
//
// Empty dir entries are skipped; duplicates are only reported once.
func ensureUnderHome(dirs []string, allowOutside bool) error {
	if allowOutside {
		fmt.Fprintln(os.Stderr, "home-guard: bypassed (--allow-outside-home)")
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home-guard: cannot determine your home directory (%v); "+
			"set HOME or pass --allow-outside-home to run anyway", err)
	}
	canonHome := pathguard.Canonical(home)

	seen := make(map[string]bool)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		canon := pathguard.Canonical(d)
		if seen[canon] {
			continue
		}
		seen[canon] = true
		if !pathguard.Under(canon, canonHome) {
			return fmt.Errorf("home-guard: %q (%s) is not inside your home directory (%s); "+
				"pass --allow-outside-home to run anyway", d, canon, canonHome)
		}
	}
	return nil
}

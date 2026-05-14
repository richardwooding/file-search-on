package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
)

// expandHomeDir expands a leading "~" or "~/..." to the current user's
// home directory. Returns the input unchanged for any other path shape
// (absolute, relative without ~, "" / "." / a path with tilde mid-string).
//
// MCP clients like Claude Desktop pass tilde-prefixed paths verbatim —
// there's no shell between the agent and the handler to do the
// expansion that a terminal user gets for free. Without this helper an
// agent invocation like `{"dir": "~/Code"}` walks a directory named
// "~", returns zero matches, and the call succeeds — confusingly — with
// an empty result.
//
// The POSIX "~user/..." form (alice's home dir) is intentionally NOT
// expanded; it adds os/user complexity for a case agents don't emit in
// practice. The CLI doesn't need this expansion — the shell expands
// "~/..." before main() ever sees it.
func expandHomeDir(p string) (string, error) {
	if p == "" || (p != "~" && !strings.HasPrefix(p, "~/")) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

// expandHomeDirs applies expandHomeDir to every entry. Returns a new
// slice (never mutates input) and the first error encountered. Used by
// the multi-root tool inputs — search.Dirs / stats.Dirs / etc.
func expandHomeDirs(ps []string) ([]string, error) {
	if len(ps) == 0 {
		return ps, nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		ex, err := expandHomeDir(p)
		if err != nil {
			return nil, err
		}
		out[i] = ex
	}
	return out, nil
}

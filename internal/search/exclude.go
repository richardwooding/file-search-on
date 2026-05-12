package search

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// excluder combines explicit glob patterns (matched against the basename
// of each path) with an optional .gitignore parsed from the walk root.
// nil-safe: a nil *excluder.Match always returns false, so callers can
// skip the construction overhead when neither feature is enabled.
type excluder struct {
	// globs are filepath.Match patterns matched against the basename
	// of each visited entry. "node_modules" / ".git" / "*.bak" are
	// typical. Path-component patterns (e.g. "**/build") are NOT
	// supported here; that's the gitignore code path.
	globs []string

	// ignore is the parsed root .gitignore. nil when RespectGitignore
	// is off or the file was absent / unreadable.
	ignore *gitignore.GitIgnore
}

// newExcluder builds an excluder from the user-provided glob patterns
// and (optionally) a .gitignore at the fsys root. Returns nil when
// neither feature is enabled, signalling "no exclusion" to the walker.
func newExcluder(fsys fs.FS, globs []string, respectGitignore bool) *excluder {
	var ignore *gitignore.GitIgnore
	if respectGitignore {
		if data, err := fs.ReadFile(fsys, ".gitignore"); err == nil {
			lines := strings.Split(string(data), "\n")
			ignore = gitignore.CompileIgnoreLines(lines...)
		}
		// Absent .gitignore is fine — RespectGitignore is a "best
		// effort" toggle, not a precondition. Other errors (perm
		// denied, etc.) also silently degrade; the walker continues
		// with whatever globs the user provided.
	}
	if len(globs) == 0 && ignore == nil {
		return nil
	}
	return &excluder{globs: globs, ignore: ignore}
}

// Match reports whether the given fs.FS-style path (forward slashes,
// relative to the walk root) is excluded. Callers should pass d.IsDir()
// for isDir; matched directories should be skipped via fs.SkipDir so
// their entire subtree is pruned rather than visited file-by-file.
func (e *excluder) Match(fsPath string, isDir bool) bool {
	if e == nil {
		return false
	}
	// Glob patterns match against the basename only — "node_modules"
	// matches a directory named node_modules anywhere in the tree
	// without users having to write "**/node_modules" (which
	// filepath.Match doesn't support anyway).
	base := path.Base(fsPath)
	for _, g := range e.globs {
		if matched, _ := filepath.Match(g, base); matched {
			return true
		}
	}
	// gitignore semantics handle path-aware matching natively. The
	// library wants OS-style paths for some matchers, but POSIX
	// works for the common cases — file-search-on always uses
	// forward-slash fs.FS paths.
	if e.ignore != nil && e.ignore.MatchesPath(fsPath) {
		return true
	}
	return false
}

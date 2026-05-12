package projecttype

import (
	"io/fs"
	"os"
	"path/filepath"
)

// readOSDir is fs.ReadDir's OS-backed counterpart. Kept separate so
// the in-memory test path (fs.FS injection) and the production path
// (direct os.ReadDir) don't share a wrapper.
func readOSDir(dir string) ([]fs.DirEntry, error) {
	return os.ReadDir(dir)
}

// excluder is a basename-glob matcher used by Find. Mirrors the
// minimal subset of internal/search/exclude.go's behaviour — full
// gitignore semantics are out of scope for project-root walking,
// where the typical excludes (.git, node_modules, target, dist) are
// just basenames.
type excluder struct {
	patterns []string
}

func newExcluder(patterns []string) *excluder {
	return &excluder{patterns: patterns}
}

func (e *excluder) skip(name string) bool {
	if e == nil {
		return false
	}
	for _, pat := range e.patterns {
		if pat == name {
			return true
		}
		if ok, err := filepath.Match(pat, name); err == nil && ok {
			return true
		}
	}
	return false
}

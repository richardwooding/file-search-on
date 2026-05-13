package projecttype

import (
	"path/filepath"
	"sync"
)

// ProjectResolver answers "what project does this file belong to?"
// by walking up the file's directory chain to the nearest ancestor
// that matches a registered ProjectType. Cached per-directory via
// sync.Map so a walk of 1000 files in one project root costs one
// Detect call, not 1000.
//
// Worker-safe: every concurrent search worker can call Resolve
// without coordination beyond the sync.Map.
type ProjectResolver struct {
	root     string
	registry *Registry
	cache    sync.Map // dir-path → []Match
}

// NewResolver returns a resolver rooted at root. Resolve will walk
// up no higher than root — files at the root with no project
// indicators return an empty match slice.
//
// reg may be nil; nil means DefaultRegistry().
func NewResolver(root string, reg *Registry) *ProjectResolver {
	if reg == nil {
		reg = defaultRegistry
	}
	return &ProjectResolver{root: filepath.Clean(root), registry: reg}
}

// Resolve walks up from filePath's directory looking for the nearest
// ancestor that matches a registered project type. Returns the
// matched types for the closest project (alphabetically sorted by
// Detect), or nil if no ancestor matches up to the walk root.
//
// Cached: each unique directory is detected at most once for the
// resolver's lifetime.
func (r *ProjectResolver) Resolve(filePath string) []Match {
	dir := filepath.Dir(filePath)
	for {
		matches := r.detectCached(dir)
		if len(matches) > 0 {
			return matches
		}
		// Stop conditions: reached the walk root, reached filesystem
		// root, or didn't progress (single-segment path on Windows).
		if dir == r.root {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// detectCached returns the cached match list for dir, populating it
// via registry.Detect on miss. Empty slice cached too — a "definitely
// not a project root" verdict is just as useful for repeat lookups
// as a positive one.
func (r *ProjectResolver) detectCached(dir string) []Match {
	if v, ok := r.cache.Load(dir); ok {
		return v.([]Match)
	}
	matches := r.registry.Detect(nil, dir)
	// LoadOrStore so a concurrent first call doesn't lose its result;
	// the cached slice is immutable so identity doesn't matter beyond
	// "we've decided".
	actual, _ := r.cache.LoadOrStore(dir, matches)
	return actual.([]Match)
}

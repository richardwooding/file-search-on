package projecttype

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"time"
)

// Detect inspects a single directory and returns the project types
// it matches. A directory can match multiple types simultaneously
// (e.g. a Go module with a docker-compose.yml). Returns an empty
// slice when no project type fires.
//
// Reads the directory's listing once (non-recursive); HasFile /
// HasGlob indicators run against file basenames; CELExpr indicators
// see both `files` and `subdirs` lists. fsys=nil uses os.ReadDir for
// the production filesystem path.
func (r *Registry) Detect(fsys fs.FS, dir string) []Match {
	files, subdirs, err := readListing(fsys, dir)
	if err != nil {
		return nil
	}
	r.mu.RLock()
	types := make([]*ProjectType, len(r.types))
	copy(types, r.types)
	r.mu.RUnlock()

	var matches []Match
	for _, t := range types {
		if ind, ok := t.match(files, subdirs); ok {
			matches = append(matches, Match{Type: t.Name, Indicator: ind.String()})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Type < matches[j].Type })
	return matches
}

// Detect is the package-level shortcut over DefaultRegistry.
func Detect(fsys fs.FS, dir string) []Match {
	return defaultRegistry.Detect(fsys, dir)
}

// FindOptions configures a recursive search for project roots under
// a starting directory.
type FindOptions struct {
	// Types, when non-empty, restricts results to projects matching
	// at least one of the named types. Empty means accept all.
	Types []string
	// Excludes is a list of basename globs that prune directories
	// during the walk. Same semantics as search.Options.Excludes.
	Excludes []string
	// Nested, when true, keeps walking inside a matched project
	// root so nested sub-projects (monorepo workspaces, vendored
	// dependencies) are also reported. Default (false) stops at the
	// first match, which is the common "find me all my Go repos"
	// shape.
	Nested bool
	// RespectGitignore parses a .gitignore at the walk root and
	// prunes matching paths. Nested .gitignore files are not
	// consulted (same caveat as search).
	RespectGitignore bool
	// Timeout bounds the walk. Zero means no timeout. On expiry,
	// FindResult.Cancelled is set with the partial result.
	Timeout time.Duration
}

// FindResult is the structured output of Find.
type FindResult struct {
	Projects           []FoundProject `json:"projects"`
	Count              int            `json:"count"`
	Cancelled          bool           `json:"cancelled,omitempty"`
	CancellationReason string         `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64        `json:"elapsed_seconds,omitempty"`
}

// FoundProject is one project root found during a Find walk.
type FoundProject struct {
	Path  string  `json:"path"`
	Types []Match `json:"types"`
}

// Find walks root recursively and returns every directory that
// matches at least one project type (subject to Options.Types). By
// default the walker does NOT descend into matched roots (Nested=false)
// — set Nested=true to also surface sub-projects.
//
// Honours ctx cancellation and Options.Timeout; on expiry the partial
// result is returned with Cancelled=true (same contract as the file
// search tool).
func (r *Registry) Find(ctx context.Context, root string, opts FindOptions) (*FindResult, error) {
	start := time.Now()

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	wantTypes := map[string]struct{}{}
	for _, t := range opts.Types {
		wantTypes[t] = struct{}{}
	}

	excluder := newExcluder(opts.Excludes)

	out := &FindResult{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission errors / vanished entries: keep walking.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && excluder.skip(filepath.Base(path)) {
			return fs.SkipDir
		}

		matches := r.Detect(nil, path)
		if len(matches) == 0 {
			return nil
		}
		if len(wantTypes) > 0 {
			matches = filterTypes(matches, wantTypes)
			if len(matches) == 0 {
				return nil
			}
		}
		out.Projects = append(out.Projects, FoundProject{Path: path, Types: matches})
		if !opts.Nested {
			return fs.SkipDir
		}
		return nil
	})
	out.Count = len(out.Projects)
	out.ElapsedSeconds = time.Since(start).Seconds()

	switch {
	case errors.Is(walkErr, context.Canceled):
		out.Cancelled = true
		out.CancellationReason = "client_cancel"
		return out, nil
	case errors.Is(walkErr, context.DeadlineExceeded):
		out.Cancelled = true
		out.CancellationReason = "timeout"
		return out, nil
	case walkErr != nil:
		return out, walkErr
	}
	return out, nil
}

// Find is the package-level shortcut over DefaultRegistry.
func Find(ctx context.Context, root string, opts FindOptions) (*FindResult, error) {
	return defaultRegistry.Find(ctx, root, opts)
}

// CollectBuildExcludes walks root and returns the sorted, deduped
// union of BuildExcludes from every project type detected at or
// below root (Nested-style — every project, not just outer roots).
// Used by the search walker when Options.PruneBuildArtefacts is set
// to pre-populate the basename excluder before the main walk
// starts, so e.g. `vendor/` inside a Go module is pruned even when
// the file-search walker would have visited it before recognising
// the project.
//
// Cheap: only directories that contain at least one project
// indicator contribute. Empty result when no projects are detected
// (or the registry has no built-ins with BuildExcludes set).
func (r *Registry) CollectBuildExcludes(ctx context.Context, root string) ([]string, error) {
	res, err := r.Find(ctx, root, FindOptions{Nested: true})
	if err != nil {
		return nil, err
	}
	// Walk the matched projects, look up each Type by name, union
	// its BuildExcludes. Build a set so duplicates (vendor for go +
	// vendor for some custom type) collapse.
	r.mu.RLock()
	byName := make(map[string]*ProjectType, len(r.types))
	for _, t := range r.types {
		byName[t.Name] = t
	}
	r.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, p := range res.Projects {
		for _, m := range p.Types {
			t, ok := byName[m.Type]
			if !ok {
				continue
			}
			for _, ex := range t.BuildExcludes {
				seen[ex] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for ex := range seen {
		out = append(out, ex)
	}
	sort.Strings(out)
	return out, nil
}

// CollectBuildExcludes is the package-level shortcut over
// DefaultRegistry.
func CollectBuildExcludes(ctx context.Context, root string) ([]string, error) {
	return defaultRegistry.CollectBuildExcludes(ctx, root)
}

// readListing returns the basenames of immediate children of dir,
// split into files and subdirs. fsys may be nil to read the OS
// filesystem directly (so callers don't have to build os.DirFS for
// every Detect call). HasFile / HasGlob indicators consult only the
// files slice; CELExpr indicators get both via the `files` /
// `subdirs` variables in the directory CEL env.
func readListing(fsys fs.FS, dir string) (files, subdirs []string, err error) {
	var entries []fs.DirEntry
	if fsys == nil {
		entries, err = readOSDir(dir)
	} else {
		entries, err = fs.ReadDir(fsys, dir)
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e.Name())
		} else {
			files = append(files, e.Name())
		}
	}
	return files, subdirs, nil
}

func filterTypes(matches []Match, want map[string]struct{}) []Match {
	out := matches[:0]
	for _, m := range matches {
		if _, ok := want[m.Type]; ok {
			out = append(out, m)
		}
	}
	return out
}

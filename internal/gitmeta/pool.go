package gitmeta

import (
	"context"
	"strings"
	"sync"
)

// Pool caches *Cache instances by canonical repository root, refreshed
// when HEAD changes. Safe for concurrent use.
//
// Designed to live for an MCP server's process lifetime so multiple
// `with_git=true` search calls against the same repo share one
// gitmeta.New() pass. HEAD invalidation runs on every Get — one
// `git rev-parse HEAD` per call (~3-5ms) — so a `git commit` or
// `git checkout` between calls is picked up without operator action.
//
// Bounded memory in practice: MCP servers are typically cwd-anchored,
// so the map holds one entry per process. No LRU cap for v1.
type Pool struct {
	mu      sync.RWMutex
	entries map[string]*poolEntry
}

// poolEntry pairs a built Cache with the HEAD sha that was current
// when it was built. The next Get re-runs `git rev-parse HEAD` and
// rebuilds if sha no longer matches.
type poolEntry struct {
	cache   *Cache
	headSHA string
}

// NewPool returns an empty Pool ready for Get / Warm calls. Allocates
// only the underlying map — building actual caches happens on first
// Get. Cheap enough that the MCP server constructs one unconditionally.
func NewPool() *Pool {
	return &Pool{entries: make(map[string]*poolEntry)}
}

// Get returns a Cache for the git tree containing root. If a cached
// entry exists for the same canonical repo root AND the current HEAD
// still matches, the cached Cache is returned unchanged. Otherwise
// (no entry, or HEAD moved since last Get) the cache is rebuilt via
// New and stored.
//
// Returns (nil, nil) when root is not inside a git tree — same
// silent-skip contract as New. nil-receiver safe (returns nil, nil)
// for callers that don't always carry a pool.
//
// Cost on a hit: one `git rev-parse HEAD` (~3-5ms) for the HEAD-sha
// staleness check.
// Cost on a miss: the full New() pass (~500ms on a 10k-file repo).
// The miss is paid only on the first call after process start or
// after a commit / checkout.
func (p *Pool) Get(ctx context.Context, root string) (*Cache, error) {
	if p == nil {
		return nil, nil
	}

	// Resolve canonical root first. This is the same call New() will
	// make on cache-miss; doing it up front lets us key the map by
	// the canonical form (so /tmp/foo and /private/tmp/foo on macOS
	// reach the same entry).
	canonical, err := repoToplevel(ctx, root)
	if err != nil {
		// Not a git tree (or `git` not on PATH) → silent skip, same
		// contract as New().
		return nil, nil //nolint:nilerr
	}
	canonical = strings.TrimSpace(canonical)

	// Cache-hit fast path: existing entry + HEAD-sha match → return.
	p.mu.RLock()
	entry, ok := p.entries[canonical]
	p.mu.RUnlock()
	if ok {
		headSHA, err := revParseHead(ctx, canonical)
		if err == nil && strings.TrimSpace(headSHA) == entry.headSHA {
			return entry.cache, nil
		}
		// HEAD moved or rev-parse failed; fall through to rebuild.
	}

	// Cache-miss path: build fresh via New(). Pass the ORIGINAL root
	// (not the canonical form) so the Cache's altRoot fallback picks
	// up the user-supplied symlinked path — required for the macOS
	// /tmp ↔ /private/tmp case where the walker emits /tmp/... paths
	// but git canonicalises to /private/tmp/... New does its own
	// repoToplevel resolution so passing root is correct.
	cache, err := New(ctx, root)
	if err != nil {
		return nil, err
	}
	if cache == nil {
		// Race: canonical resolved a moment ago but New saw something
		// different. Surface as silent skip rather than error.
		return nil, nil
	}

	p.mu.Lock()
	p.entries[canonical] = &poolEntry{
		cache:   cache,
		headSHA: cache.HeadSHA(),
	}
	p.mu.Unlock()
	return cache, nil
}

// Warm is Get-discard: pre-build the cache for root so the first
// search call doesn't pay the gitmeta.New() cost. Reuses Get's
// HEAD-validation logic, so a stale entry refreshes automatically.
// Returns the same nil-for-non-git-tree contract as Get; callers
// typically ignore the error and log the elapsed time.
func (p *Pool) Warm(ctx context.Context, root string) error {
	_, err := p.Get(ctx, root)
	return err
}

// Len returns the number of cached entries. Exposed for tests and
// future diagnostics (e.g. monitor_info "git pool: N repos").
func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

// Package index provides an optional cache of per-file content-type
// attributes, keyed by absolute path and validated by (size, mtime).
// It exists so repeated searches against an unchanged tree can skip
// the expensive ContentType.Attributes parse step.
//
// Two implementations are exposed via the Index interface:
//
//   - in-memory (NewMemory): a sync.RWMutex-protected map; used by the
//     MCP server's auto-on path where caching lives only for the server
//     process lifetime.
//   - on-disk (Open with a non-empty path): a single bbolt file under
//     bucket attrs_v1; used by the CLI's --index-path opt-in.
//
// Both validators use the (size, mtime) pair: a Lookup with mismatched
// size or mtime is a miss. Stale entries (files that no longer exist
// in the walked tree) are simply never read; lazy GC.
package index

import (
	"errors"
	"time"
)

// Entry is the cached payload for one file.
//
// ModTimeUnixNano is stored alongside Size as the validation tuple.
// ContentType is the registered type's Name() (e.g. "markdown",
// "image/jpeg"); empty when no type matched. Extra mirrors
// content.Attributes — a map[string]any of primitives, []string,
// time.Time, and nested map[string]any (frontmatter). Treat the map
// as read-only after a Lookup.
type Entry struct {
	Size            int64
	ModTimeUnixNano int64
	ContentType     string
	Extra           map[string]any
}

// Stats counts cache events for diagnostic surfacing (CLI footer, MCP
// index_stats tool, tests). All fields are monotonic; no Reset.
type Stats struct {
	Hits   uint64
	Misses uint64
	Puts   uint64
	Stales uint64
	Errors uint64
}

// Index is the surface every cache implementation honours.
//
// Lookup returns an Entry pointer + true on a validated hit (size and
// mtime both match). On miss or stale or any internal error, it
// returns false. Implementations bump Stats counters internally.
//
// Put stores an Entry for the given absolute path; failures
// (encoding, full write channel, oversized payload) increment
// Stats.Errors but never block the caller.
//
// Stats returns a snapshot.
//
// Close releases resources (closes the bbolt db, drains the writer
// goroutine). Safe to call once on each Index; subsequent calls are
// no-ops.
type Index interface {
	Lookup(absPath string, size int64, mtime time.Time) (*Entry, bool)
	Put(absPath string, e *Entry) error
	Stats() Stats
	Close() error
}

// ErrSchemaMismatch is returned by Open when the on-disk index file
// belongs to a different (older or newer) schema version than this
// binary understands. The CLI surfaces a "delete or pass a new
// --index-path" message; we never auto-delete user data.
var ErrSchemaMismatch = errors.New("index: schema version mismatch (delete the file or pass a new --index-path)")

// NewMemory returns an in-memory Index. It is concurrent-safe and has
// no persistence; suitable for the MCP server's auto-on cache.
func NewMemory() Index {
	return newMemoryIndex()
}

// Open returns an Index. When path is empty it returns NewMemory().
// Otherwise it opens (or creates) a bbolt file at path. On
// schema-version mismatch it returns ErrSchemaMismatch and does NOT
// modify the file.
func Open(path string) (Index, error) {
	if path == "" {
		return NewMemory(), nil
	}
	return openBoltIndex(path)
}

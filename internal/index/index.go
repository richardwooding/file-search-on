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
	// Hash is the sha256 hex digest of the file's content. Empty
	// unless something asked for it (e.g. the duplicates tool).
	// Cached alongside ContentType so repeat duplicate-detection
	// passes don't have to re-read files. Hash is invariant under
	// (size, mtime), the same validation tuple the rest of the
	// entry uses, so a cache hit on a file is also a hash hit.
	// gob handles the additive field gracefully — older cache
	// files without this field decode with Hash="".
	Hash string
	// MD5, SHA1 are companion hex digests populated alongside Hash
	// when callers request hashing — single-pass io.MultiWriter so
	// the file is read once for all three. Forensic interop:
	// MD5 + SHA1 are the canonical NSRL / VirusTotal / Autopsy /
	// EnCase indexes; SHA256 (the existing Hash field) is the
	// modern default. gob-additive — pre-#143 cache entries decode
	// with MD5="" / SHA1="" and the next hash-requiring pass
	// repopulates them.
	MD5  string
	SHA1 string
	// MagicContentType, ExtensionContentType cache the results of
	// Registry.DetectBoth — what the file's bytes look like under
	// magic-byte sniffing alone vs what its extension implies. Set
	// only when the caller opts in via BuildOptions.CheckDisguised
	// (PR #145). Either string may legitimately be empty (no magic
	// match / extensionless file) — use DisguiseChecked to tell
	// "detected and empty" from "never detected". gob-additive.
	MagicContentType     string
	ExtensionContentType string
	DisguiseChecked      bool
	// Fingerprint is the 64-bit Charikar SimHash of the file's
	// extracted body, computed by internal/fingerprint. Zero unless
	// the near-duplicates pipeline asked for it. Like Hash, it's
	// invariant under (size, mtime) — the cached fingerprint stays
	// valid for as long as the entry validates. gob-additive: older
	// caches without this field decode with Fingerprint=0, which
	// the near-duplicates path treats as "not fingerprinted yet"
	// and re-computes on the next access.
	Fingerprint uint64
	// EntryAttributes carries the per-entry attribute records for an
	// archive file's contents, used by the find-in-archive tools to
	// avoid re-walking and re-detecting on repeat queries. Nil
	// unless something populated it (the regular search and
	// find_duplicates paths don't touch this field). Validated
	// against the OUTER archive's (size, mtime) — any change to the
	// archive invalidates the entire entry list. Archives with more
	// than archiveCacheMaxEntries entries skip this cache because
	// the encoded slice would blow past the 256 KiB soft cap.
	// gob-additive: older caches decode with EntryAttributes=nil.
	EntryAttributes []EntryRecord
}

// EntryRecord caches one archive entry's identity and attributes.
// Used by index.Entry.EntryAttributes. The ContentType + Extra
// fields are what celexpr's CEL evaluator needs to evaluate a
// filter against a cached entry without re-reading the archive.
type EntryRecord struct {
	Name            string
	Size            int64
	ModTimeUnixNano int64
	ContentType     string
	Extra           map[string]any
}

// BodyEntry caches the extracted text body of a file.
//
// Validation tuple is (Size, ModTimeUnixNano) — same as Entry, so a
// (size, mtime) change on disk invalidates the body cache in lockstep
// with the attribute cache. CreatedUnixNano marks the wall-clock time
// the body was extracted and stored; it's stable and informational.
//
// Body holds the extracted plain-text body string (truncated to the
// per-body cap). For text-shaped types this is the raw bytes; for
// structured types (PDF / office / EPUB / email) it's the
// content.ExtractBody output.
//
// Access timestamps for LRU eviction live in a separate small bucket
// (body_access_v1) keyed by the same path; touching on read updates
// only the access bucket so a touch doesn't re-encode the body.
type BodyEntry struct {
	Size            int64
	ModTimeUnixNano int64
	CreatedUnixNano int64
	Body            string
}

// Stats counts cache events for diagnostic surfacing (CLI footer, MCP
// index_stats tool, tests). All fields are monotonic; no Reset.
//
// Body* counters are the body-cache analogues of the attribute-cache
// counters. They report independently because body cache effectiveness
// and attribute cache effectiveness can diverge — a workload that
// always asks for body but rarely re-queries the same path will see
// high BodyMisses without affecting Hits/Misses on attributes.
type Stats struct {
	Hits   uint64
	Misses uint64
	Puts   uint64
	Stales uint64
	Errors uint64

	BodyHits      uint64 // successful LookupBody
	BodyMisses    uint64 // LookupBody with no entry
	BodyPuts      uint64 // PutBody persisted to disk
	BodyStales    uint64 // LookupBody (size, mtime) mismatch
	BodyEvictions uint64 // entries dropped by FIFO eviction when over cap
	BodyOversize  uint64 // PutBody encoded > bodyMaxBytes; dropped
	// BodyErrors: encode failures, full body-puts channel (peak walker
	// bursts on cold trees can outrun the writer's drain rate), or
	// failed body-writer batch transactions. A high count next to a
	// healthy BodyHits is benign — affected bodies simply re-extract
	// next call. Worth investigating only when BodyHits stays near zero.
	BodyErrors uint64
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

	// LookupBody returns the cached extracted body for absPath when the
	// (size, mtime) tuple matches. A hit also bumps the LRU access
	// timestamp so frequently-used bodies stay out of the eviction
	// queue.
	LookupBody(absPath string, size int64, mtime time.Time) (string, bool)

	// PutBody stores an extracted body for absPath. The implementation
	// is responsible for honouring size caps and triggering eviction
	// when the body cache exceeds its configured maximum. Failures
	// (encoded size > bodyMaxBytes, channel full, encode error) bump
	// the relevant Body* counter but never return an error to the
	// caller — the body simply isn't cached, which is recoverable
	// (a re-extraction next time).
	PutBody(absPath string, e *BodyEntry) error

	Stats() Stats
	Close() error
}

// BodyCacheCap is the optional configuration for body cache size
// limits. Passed to Open via Options when the caller wants a non-
// default cap or opt-out. Zero values mean "use default" (256 MiB
// cap, body cache enabled).
type BodyCacheCap struct {
	MaxBytes int64 // total bytes for bodies_v1 bucket; 0 = default
	Disable  bool  // when true, PutBody is a no-op and LookupBody always misses
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

// Open returns an Index using the default body-cache cap (256 MiB).
// When path is empty it returns NewMemory(). Otherwise it opens (or
// creates) a bbolt file at path. On schema-version mismatch it returns
// ErrSchemaMismatch and does NOT modify the file.
func Open(path string) (Index, error) {
	return OpenWith(path, BodyCacheCap{})
}

// OpenWith is Open with a tunable body-cache cap.
func OpenWith(path string, cap BodyCacheCap) (Index, error) {
	if path == "" {
		return NewMemory(), nil
	}
	return openBoltIndex(path, cap)
}

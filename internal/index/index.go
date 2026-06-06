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
	// Vector is the file body's embedding vector (L2-normalised),
	// populated when BuildOptions.SemanticQuery + Embedder are set
	// (issue #151). Cached under the same (size, mtime) validation
	// tuple as the rest of the entry. The dimension depends on the
	// embedding model (768 for nomic-embed-text, 1024 for
	// mxbai-embed-large, etc.) — switching models on a tree that
	// was previously embedded with a different model would produce
	// nonsensical similarity scores, so populateSimilarity validates
	// EmbedModel below before reusing the cached vector. gob-additive:
	// pre-#151 entries decode with empty Vector and the next
	// semantic walk repopulates them.
	Vector []float32
	// ChunkVectors holds one L2-normalised embedding per body chunk
	// (issue #332). Whole-document semantic search embeds the body in
	// context-window-sized chunks and scores a document by the MAX
	// cosine over its chunks, so a relevant passage deep in a long
	// document still ranks — the single Vector above only ever covered
	// the opening (the #305 cap). ChunkVectors supersedes Vector: the
	// live pipeline reads ChunkVectors and treats a legacy single
	// Vector (no ChunkVectors) as "not chunked yet" → re-embed.
	// gob-additive.
	ChunkVectors [][]float32
	// EmbedModel is the model name that produced Vector / ChunkVectors.
	// Empty when none are set OR when the vector came from a pre-#154
	// cache entry (in which case populateSimilarity treats it as a
	// mismatch and re-embeds — we never trust a vector of unknown
	// provenance). gob-additive.
	EmbedModel string
	// Fingerprint is the legacy 64-bit Charikar SimHash field from
	// the v0.x near-duplicates pipeline. Computed over the raw
	// extracted body — without the per-language boilerplate strip
	// that issue #274 added. Kept on the struct for back-compat
	// (gob decoders for older bbolt indexes still find their data)
	// but NO LONGER READ by the live pipeline. New writers populate
	// FingerprintV2 instead. Will be removed in a future major.
	Fingerprint uint64
	// FingerprintV2 is the post-#274 SimHash — computed AFTER
	// preprocessForFingerprint strips the language's leading
	// comment block and package / import scaffolding, but still over
	// single-word tokens. Superseded by FingerprintV3; kept for
	// back-compat decode of pre-#310 caches but NO LONGER READ.
	FingerprintV2 uint64
	// FingerprintV3 is the post-#310 SimHash — computed over k-word
	// SHINGLES rather than single tokens. Single-token SimHash made
	// all natural-language prose look ~90% similar (the high-frequency
	// stopword distribution is near-universal across English text), so
	// unrelated books clustered together. Shingling keys the
	// fingerprint on phrasing, which is document-specific. Zero on
	// older cache entries (gob-additive); the near-duplicates path
	// treats zero as "not fingerprinted yet" and recomputes. The
	// single-token V2 values are NOT reused — they're incomparable
	// with V3 and would reproduce the #310 false positives.
	FingerprintV3 uint64
	// PHash is the 64-bit perceptual hash of the IMAGE pixels (DCT
	// over an 8×8 low-frequency block, median-threshold). Populated
	// only for image/* content types when the caller opts in via
	// BuildOptions.WithPHash. Zero for non-images and for entries
	// where pHash hasn't been computed yet — agents can detect "no
	// pHash" via `phash == ""` since the wire format is the hex
	// encoding of this u64. Stable under (size, mtime) — the pixels
	// haven't changed, so the pHash hasn't either. gob-additive:
	// pre-#208 entries decode with PHash=0 and the next pHash pass
	// repopulates them. Issue #208.
	PHash uint64
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
	// EntryOversize counts Puts dropped because the encoded Entry exceeded
	// maxEntryBytes — a recoverable cache miss, but a non-zero value means
	// a real payload (e.g. chunked high-dim embedding vectors, #346) can't
	// persist and is re-computed every run. Distinct from Errors so the
	// silent drop is visible in index_stats. bbolt-only (the in-memory
	// backend stores *Entry without encoding, so it has no size cap). #348.
	EntryOversize uint64

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

	// Embed* counters track the embedding cache (PR for #151).
	// Embed cache lives in index.Entry.Vector alongside the other
	// per-file fields and uses the same (size, mtime) validation.
	EmbedHits            uint64 // successful Vector reuse from cache
	EmbedMisses          uint64 // cache had no Vector → had to call the Embedder
	EmbedPuts            uint64 // freshly-computed vector stored back to cache
	EmbedErrors          uint64 // Embedder.Embed call failed (Ollama unreachable, model missing, etc.)
	EmbedModelMismatches uint64 // cached Vector existed but came from a different embedding model → treated as miss, re-embedded

	// AttrEntriesCount / BodyEntriesCount are the *current* number of
	// cached records in each bucket — distinct from the monotonic Hits
	// / Puts counters above. They give the dashboard a "size of cache"
	// gauge alongside the activity gauges. The bbolt implementation
	// computes these on-demand via bucket.Stats(); the in-memory
	// implementation reports map length.
	AttrEntriesCount uint64
	BodyEntriesCount uint64
	// BodiesTotalBytes mirrors the in-memory running size used by FIFO
	// eviction (bbolt only — the in-memory backend doesn't track this
	// because it has no per-size eviction). Reported in bytes so the
	// dashboard can render "256 MiB cap / 87.4 MiB used".
	BodiesTotalBytes int64
}

// EntrySummary is the compact row shape returned by ListAttrs. The
// full Entry payload (Extra map, hashes, vector) is fetched via
// PeekAttrs on demand for a detail view — list responses avoid
// shipping the heavy fields.
type EntrySummary struct {
	Path        string    `json:"path"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	// Stale is true when the live file's (size, mtime) no longer
	// matches the cached entry's. The list builder stats every
	// candidate to compute this — a one-shot Lstat per row is cheap
	// against the size of a typical browser page (50 entries).
	// Bumps to true silently when the underlying file vanishes.
	Stale bool `json:"stale,omitempty"`
}

// BodySummary is the compact row shape returned by ListBodies.
type BodySummary struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// LastAccess is the most-recent read time recorded for FIFO
	// eviction. Zero for the in-memory backend (no eviction layer).
	LastAccess time.Time `json:"last_access"`
	Stale      bool      `json:"stale,omitempty"`
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

	// BumpEmbedStat increments one of the embedding cache counters.
	// kind is "hit" / "miss" / "put" / "error" — anything else is a
	// no-op. Separate from Lookup/Put because the embedding cache
	// hit/miss decision lives in the caller (celexpr) — Vector is
	// just a field on Entry, but agents want a per-cache breakdown
	// of how often the field was present.
	BumpEmbedStat(kind string)

	// ListAttrs returns summaries for entries in the attribute cache
	// whose path contains substr (empty = match all), sorted by path
	// lexicographically, sliced by offset + limit. Total is the
	// unfiltered count after the substring pass — pagination UIs use
	// it to render the right "showing N of M" footer.
	//
	// Implementations need not enforce a limit themselves (callers can
	// pass a huge value); a sane upper bound is enforced at the HTTP
	// layer (see internal/monitor/server.go) to keep response sizes
	// bounded.
	ListAttrs(substr string, limit, offset int) ([]EntrySummary, int, error)

	// ListBodies returns summaries for body-cache entries. Same shape
	// semantics as ListAttrs.
	ListBodies(substr string, limit, offset int) ([]BodySummary, int, error)

	// PeekAttrs returns the cached Entry for absPath bypassing
	// (size, mtime) validation. The dashboard's detail view uses this
	// to show stale entries with a stale flag rather than hiding
	// them. Walking callers continue to use the strict Lookup.
	PeekAttrs(absPath string) (*Entry, bool)

	// PeekBody returns the cached BodyEntry for absPath bypassing
	// validation. Same semantics as PeekAttrs.
	PeekBody(absPath string) (*BodyEntry, bool)

	// Delete removes the attr + body + body-access entries for
	// absPath in a single backend operation (bbolt uses one tx; the
	// in-memory backend deletes from both maps under the same lock).
	// Returns nil when no entry was present — Delete is idempotent.
	// Stats counters are NOT decremented (they're monotonic-since-
	// process-start; reset on process restart).
	//
	// Surfaced by the monitoring dashboard's per-row Evict button
	// (PR added with #265-era follow-up) for operators inspecting
	// cache contents and wanting to drop a specific entry.
	Delete(absPath string) error

	// Clear wipes every cached entry across attrs / bodies /
	// body-access. For bbolt this is DeleteBucket + re-create in a
	// single tx; for in-memory it re-inits the maps. Stats counters
	// stay monotonic-since-process-start.
	//
	// Surfaced by the monitoring dashboard's Clear button. Hard
	// reset for "the cache feels weirdly stale" investigation.
	Clear() error

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

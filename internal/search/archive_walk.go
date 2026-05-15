package search

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// ArchiveSeparator is the character used between archive path and
// entry path in synthetic display paths. "#" matches the convention
// agents commonly use ("/path/to.zip#entry.txt") and doesn't appear
// in either real filesystem paths or archive entry names in practice.
const ArchiveSeparator = "#"

// ArchiveEntryResult is one entry surfaced by WalkArchiveEntries.
// Mirrors the search.Match shape where applicable. DisplayPath is
// "archive#entry" so agents can grep, sort, and reference entries
// without inventing their own joiner.
type ArchiveEntryResult struct {
	ArchivePath    string         `json:"archive_path"`
	Name           string         `json:"name"`
	DisplayPath    string         `json:"display_path"`
	Size           int64          `json:"size"`
	CompressedSize int64          `json:"compressed_size,omitempty"`
	ModTime        time.Time      `json:"mod_time"`
	IsDir          bool           `json:"is_dir,omitempty"`
	Mode           uint32         `json:"mode,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
}

// ArchiveWalkOptions configures WalkArchiveEntries. Defaults are
// sensible for "list me everything" calls; explicit values let
// callers narrow the surface.
type ArchiveWalkOptions struct {
	// Expr is an optional CEL expression evaluated against per-entry
	// synthetic FileAttributes. Empty means "match every entry".
	Expr string
	// Glob is a filepath.Match basename pattern applied BEFORE the
	// (expensive) per-entry Attributes pass. Empty means no glob
	// filter — every entry hits the Attributes path.
	Glob string
	// IncludeAttributes, when true, populates ArchiveEntryResult.Attributes
	// with the full per-entry attribute map. Off by default — agents
	// often just want path + size + content_type.
	IncludeAttributes bool
	// IncludeBody, when true, makes per-entry attribute extraction
	// read the entry's full bytes into a synthetic MapFS so body-based
	// CEL filters (body.contains / body.matches) fire. Capped at
	// EntryReadCap. Bypasses the entry-list cache (bodies aren't
	// cached, by design — they're large and CEL filters want fresh
	// reads).
	IncludeBody bool
	// EntryReadCap caps the per-entry bytes read into the synthetic
	// fs.FS (controls memory usage). Zero means the package default
	// (8 MiB — enough to fit typical PDF / DOCX / EPUB / email
	// bodies). Raise for archives containing huge documents; lower
	// if memory pressure matters on large tree walks.
	EntryReadCap int64
	// MaxEntries caps the number of matching entries returned. Zero
	// means unlimited. Truncated=true in the result tells callers
	// they hit the cap.
	MaxEntries int
	// Index, when non-nil, caches the per-archive entry-attribute
	// list. Cache key is the archive's absolute path; validated by
	// (size, mtime) of the OUTER archive. Hit path filters the
	// cached records by Glob + Expr without opening the archive.
	// Bypassed when IncludeBody is true.
	Index index.Index
}

// ArchiveWalkResult is the aggregate output. Truncated fires when
// MaxEntries was hit. Cancelled / CancellationReason mirror the
// search and stats tools' partial-result fields.
type ArchiveWalkResult struct {
	Entries            []ArchiveEntryResult
	Truncated          bool
	Cancelled          bool
	CancellationReason string
	ScannedEntries     int64 // total entries iterated (pre-glob, pre-CEL)
	MatchedEntries     int64 // total entries that passed glob + CEL
	CacheHit           bool  // true when filtered from cached records
}

// ArchiveCacheMaxEntries caps the number of entries an archive can
// have before the entry-list cache is skipped. The 256 KiB encoded
// payload cap in the bbolt index store would trip on larger archives
// (each EntryRecord is ~hundred bytes encoded; 10k entries ≈ 1 MB).
// Agents asking about huge archives pay the walk cost every time.
const ArchiveCacheMaxEntries = 10_000

// defaultEntryReadCap is the per-entry byte cap when none is set.
// 8 MiB — enough to fit typical PDF / DOCX / EPUB / email fixtures
// in memory so the structured-document body extractors have
// something to chew on. The top-level body reader uses 1 MiB
// because text-shaped types rarely benefit from more, but
// structured documents need their full ZIP / PDF envelope to
// extract meaningful body text. Memory cost per walk is bounded
// by this × (1 archive at a time × 1 entry at a time) — sequential
// reading keeps peak RSS modest.
const defaultEntryReadCap = 8 << 20

// WalkArchiveEntries opens archivePath, dispatches to the right
// archive iterator based on detected content type, and emits an
// ArchiveEntryResult per entry that passes the optional glob + CEL
// filters.
//
// Per-entry attribute extraction works by:
//  1. Reading up to opts.EntryReadCap bytes of the entry's content
//     into memory.
//  2. Wrapping that buffer in content.NewSingleFileFS — a 1-file
//     fs.FS whose embedded *bytes.Reader satisfies io.ReaderAt, so
//     openReaderAt and the structured-document body extractors
//     (pdfBody / ooxmlBody / epubBody / emlBody) all work against
//     it without modification.
//  3. Calling celexpr.BuildAttributesWith with cache disabled — the
//     attribute index keys on absolute OS paths and an
//     "archive#entry" pseudo-path would silently collide with no
//     real file. Caching of the entry list itself happens one layer
//     up via index.Entry.EntryAttributes (separate code path).
//  4. Evaluating the optional CEL expression against the resulting
//     FileAttributes.
//
// The singleFileFS approach reuses every existing content-type
// parser (markdown, json, xml, source/*, pdf, office/*, epub,
// email/*, etc.) without needing a Detect-from-bytes API. Body
// extraction inside archives is supported for every type the
// top-level walker supports.
func WalkArchiveEntries(ctx context.Context, archivePath string, opts ArchiveWalkOptions, registry *content.Registry) (*ArchiveWalkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Identify the archive's content type. The detector's longest-
	// suffix rule already handles `.tar.gz` vs `.gz` correctly.
	dir, base := filepath.Split(archivePath)
	if dir == "" {
		dir = "."
	}
	fsys := os.DirFS(dir)
	ct := registry.Detect(fsys, base)
	if ct == nil {
		return nil, errors.New("WalkArchiveEntries: " + archivePath + " is not a recognised archive")
	}
	name := ct.Name()
	switch name {
	case "archive/zip", "archive/tar", "archive/tar+gzip", "archive/gzip":
		// supported
	default:
		return nil, errors.New("WalkArchiveEntries: content type " + name + " is not a supported archive format")
	}

	cap := opts.EntryReadCap
	if cap <= 0 {
		cap = defaultEntryReadCap
	}

	// Compile the CEL expression once. Empty expr → matches everything.
	var evaluator *celexpr.Evaluator
	if opts.Expr != "" && opts.Expr != "true" {
		ev, err := celexpr.New(opts.Expr)
		if err != nil {
			return nil, err
		}
		evaluator = ev
	}

	// Cache hit path: when the outer archive's (size, mtime) match
	// a cached EntryAttributes list, filter it via Glob + Expr
	// without opening the archive. Bypassed when IncludeBody is set
	// (bodies aren't cached) and when the archive is on a non-
	// addressable filesystem.
	archiveAbs, _ := filepath.Abs(archivePath)
	archiveAbs = filepath.Clean(archiveAbs)
	var archiveStat os.FileInfo
	if archiveAbs != "" {
		archiveStat, _ = os.Stat(archiveAbs)
	}
	if opts.Index != nil && !opts.IncludeBody && archiveStat != nil && archiveAbs != "" {
		if cached, ok := opts.Index.Lookup(archiveAbs, archiveStat.Size(), archiveStat.ModTime()); ok && cached.EntryAttributes != nil {
			return filterCachedArchiveEntries(archivePath, cached.EntryAttributes, opts, evaluator), nil
		}
	}

	result := &ArchiveWalkResult{}
	var cacheRecords []index.EntryRecord
	if archiveStat != nil && opts.Index != nil && !opts.IncludeBody {
		// Collect records as we walk; emitted to cache at the end.
		cacheRecords = make([]index.EntryRecord, 0, 64)
	}
	visitor := func(e content.ArchiveEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		result.ScannedEntries++

		// Skip directory markers — agents asking "find file X inside
		// this archive" don't want dir entries.
		if e.IsDir {
			return nil
		}

		// Cheap glob pre-prune before the expensive Attributes pass.
		if opts.Glob != "" {
			matched, mErr := filepath.Match(opts.Glob, filepath.Base(e.Name))
			if mErr != nil {
				return mErr
			}
			if !matched {
				return nil
			}
		}

		// Read the entry's bytes into memory, capped. Reading is
		// unavoidable: detection sniffs the first 512 bytes;
		// Attributes for text-shaped types scans line-by-line;
		// body filtering needs the prefix.
		rc, err := e.Open()
		if err != nil {
			// Per-entry read failure isn't fatal for the whole walk.
			return nil
		}
		buf, _ := io.ReadAll(io.LimitReader(rc, cap))
		_ = rc.Close()

		// Single-file synthetic fs.FS exposing the entry's bytes at
		// e.Name. Lets us reuse the existing fs.FS-shaped Detect +
		// Attributes pipeline (which knows nothing about archives)
		// without depending on testing/fstest from production code.
		// ModTime / Mode preserved so per-content-type parsers that
		// care about timestamps see real values.
		entryFS := content.NewSingleFileFS(e.Name, buf, e.ModTime, e.Mode)
		displayPath := archivePath + ArchiveSeparator + e.Name

		// Cache disabled (Index: nil) for the per-entry call — the
		// archive-walk cache is at the outer archive layer.
		attrs, err := celexpr.BuildAttributesWith(ctx, entryFS, e.Name, displayPath, registry, celexpr.BuildOptions{
			IncludeBody: opts.IncludeBody,
		})
		if err != nil {
			return nil
		}

		// Cache record: collected for every entry we successfully
		// detect (whether or not it passes the CEL filter), so the
		// cached list is filter-agnostic and reusable across calls.
		if cacheRecords != nil {
			extraForCache := stripBodyFromExtra(attrs.Extra)
			cacheRecords = append(cacheRecords, index.EntryRecord{
				Name:            e.Name,
				Size:            e.Size,
				ModTimeUnixNano: e.ModTime.UnixNano(),
				ContentType:     attrs.ContentType,
				Extra:           extraForCache,
			})
		}

		// CEL evaluation when an expression is set.
		if evaluator != nil {
			match, evalErr := evaluator.Evaluate(attrs)
			if evalErr != nil || !match {
				return nil
			}
		}

		entry := ArchiveEntryResult{
			ArchivePath:    archivePath,
			Name:           e.Name,
			DisplayPath:    displayPath,
			Size:           e.Size,
			CompressedSize: e.CompressedSize,
			ModTime:        e.ModTime,
			IsDir:          e.IsDir,
			Mode:           uint32(e.Mode),
			ContentType:    attrs.ContentType,
		}
		if opts.IncludeAttributes && attrs.Extra != nil {
			entry.Attributes = sanitiseExtraForWire(attrs.Extra)
		}
		result.Entries = append(result.Entries, entry)
		result.MatchedEntries++

		if opts.MaxEntries > 0 && len(result.Entries) >= opts.MaxEntries {
			result.Truncated = true
			return content.ErrStopIteration
		}
		return nil
	}

	iterErr := content.IterateArchive(ctx, fsys, base, name, visitor)
	result.Cancelled, result.CancellationReason = classifyCancellation(iterErr, ctx)
	if iterErr != nil && !result.Cancelled && !errors.Is(iterErr, content.ErrStopIteration) {
		return result, iterErr
	}

	// Async-cache the entry-attribute list when small enough.
	if cacheRecords != nil && !result.Cancelled && !result.Truncated && len(cacheRecords) <= ArchiveCacheMaxEntries {
		merged := &index.Entry{
			Size:            archiveStat.Size(),
			ModTimeUnixNano: archiveStat.ModTime().UnixNano(),
			EntryAttributes: cacheRecords,
		}
		// Preserve any existing per-archive cache data (the outer
		// archive's content_type attributes etc.).
		if cached, ok := opts.Index.Lookup(archiveAbs, archiveStat.Size(), archiveStat.ModTime()); ok {
			merged.ContentType = cached.ContentType
			merged.Extra = cached.Extra
			merged.Hash = cached.Hash
			merged.Fingerprint = cached.Fingerprint
		}
		_ = opts.Index.Put(archiveAbs, merged)
	}
	return result, nil
}

// filterCachedArchiveEntries runs the glob + CEL filter over a
// previously-cached EntryAttributes slice. Returns a synthetic
// ArchiveWalkResult marked CacheHit=true. Bodies aren't cached so
// CEL filters using body.contains / body.matches will silently miss
// — IncludeBody=true callers bypass this path entirely (handled at
// the call site).
func filterCachedArchiveEntries(archivePath string, records []index.EntryRecord, opts ArchiveWalkOptions, evaluator *celexpr.Evaluator) *ArchiveWalkResult {
	result := &ArchiveWalkResult{CacheHit: true}
	for _, rec := range records {
		result.ScannedEntries++

		// Glob pre-prune.
		if opts.Glob != "" {
			matched, mErr := filepath.Match(opts.Glob, filepath.Base(rec.Name))
			if mErr != nil || !matched {
				continue
			}
		}

		// CEL evaluation against synthesised FileAttributes.
		if evaluator != nil {
			modTime := time.Unix(0, rec.ModTimeUnixNano)
			displayPath := archivePath + ArchiveSeparator + rec.Name
			ext := strings.ToLower(filepath.Ext(rec.Name))
			attrs := celexpr.AssembleAttributes(
				filepath.Base(rec.Name),
				displayPath,
				filepath.Dir(rec.Name),
				ext,
				rec.ContentType,
				rec.Size,
				modTime,
				content.Attributes(rec.Extra),
			)
			match, evalErr := evaluator.Evaluate(attrs)
			if evalErr != nil || !match {
				continue
			}
		}

		entry := ArchiveEntryResult{
			ArchivePath: archivePath,
			Name:        rec.Name,
			DisplayPath: archivePath + ArchiveSeparator + rec.Name,
			Size:        rec.Size,
			ModTime:     time.Unix(0, rec.ModTimeUnixNano),
			ContentType: rec.ContentType,
		}
		if opts.IncludeAttributes && rec.Extra != nil {
			entry.Attributes = sanitiseExtraForWire(content.Attributes(rec.Extra))
		}
		result.Entries = append(result.Entries, entry)
		result.MatchedEntries++

		if opts.MaxEntries > 0 && len(result.Entries) >= opts.MaxEntries {
			result.Truncated = true
			break
		}
	}
	return result
}

// stripBodyFromExtra returns a copy of extra with the "body" key
// removed. Bodies are large and not cacheable — they're re-read on
// demand for the cache-miss path; the cache-hit path can't fire
// body filters and bypasses to a re-walk via opts.IncludeBody.
func stripBodyFromExtra(extra content.Attributes) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		if k == "body" {
			continue
		}
		out[k] = v
	}
	return out
}

// sanitiseExtraForWire returns a copy of attrs.Extra suitable for JSON
// serialisation. The Extra map can contain time.Time values (which
// serialise as RFC3339), []string (which serialise as JSON arrays),
// and map[string]any (which serialise as nested objects). All Go
// values that the standard json encoder handles correctly.
//
// Body is preserved — callers reach this function only when they've
// explicitly asked for include_attributes, and they typically also
// want to see what their body.contains/body.matches filters matched
// against. The cache layer strips body separately (see
// stripBodyFromExtra) — that's the right place for the "bodies are
// never cached" rule.
func sanitiseExtraForWire(extra content.Attributes) map[string]any {
	out := make(map[string]any, len(extra))
	maps.Copy(out, extra)
	return out
}

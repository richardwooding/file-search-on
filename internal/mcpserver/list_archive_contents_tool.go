package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ListArchiveContentsInput is the JSON-schema input for the
// `list_archive_contents` tool.
type ListArchiveContentsInput struct {
	Path              string   `json:"path" jsonschema:"Path to the archive file (.zip / .tar / .tar.gz / .gz). Required."`
	Expr              string   `json:"expr,omitempty" jsonschema:"Optional CEL expression evaluated against per-entry attributes. Same CEL vocabulary as the top-level search tool — every is_X predicate (is_source, is_dockerfile, …) and per-family attribute (loc, language, page_count, frontmatter, …) works inside archives. Empty matches every entry."`
	Glob              string   `json:"glob,omitempty" jsonschema:"Optional filepath.Match basename pattern applied BEFORE the CEL filter as a cheap pre-prune (e.g. '*.go')."`
	IncludeAttributes bool     `json:"include_attributes,omitempty" jsonschema:"When true, populate each entry's 'attributes' map with the full per-format attribute set. Off by default for terse listings."`
	IncludeBody       bool     `json:"include_body,omitempty" jsonschema:"When true, read entry bodies into memory so body.contains() / body.matches() CEL filters fire. Bypasses the entry-list cache (bodies aren't cached). Capped at entry_read_cap."`
	EntryReadCap      int64    `json:"entry_read_cap,omitempty" jsonschema:"Cap on per-entry bytes read into memory for detection and body evaluation. Zero uses the 1 MiB default."`
	MaxEntries        int      `json:"max_entries,omitempty" jsonschema:"Cap on entries returned. Zero = unlimited. truncated=true in the response when hit."`
	TimeoutSeconds    *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool — partial results return on expiry with cancelled=true."`
}

// ListArchiveContentsOutput mirrors search.ArchiveWalkResult with a
// JSON-tagged wire shape.
type ListArchiveContentsOutput struct {
	Entries            []ArchiveEntryWire `json:"entries"`
	ScannedEntries     int64              `json:"scanned_entries"`
	MatchedEntries     int64              `json:"matched_entries"`
	Truncated          bool               `json:"truncated,omitempty"`
	CacheHit           bool               `json:"cache_hit,omitempty"`
	Cancelled          bool               `json:"cancelled,omitempty"`
	CancellationReason string             `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64            `json:"elapsed_seconds,omitempty"`
}

// ArchiveEntryWire is one entry on the wire.
type ArchiveEntryWire struct {
	ArchivePath    string         `json:"archive_path"`
	Name           string         `json:"name"`
	DisplayPath    string         `json:"display_path"`
	Size           int64          `json:"size"`
	CompressedSize int64          `json:"compressed_size,omitempty"`
	ModTime        string         `json:"mod_time,omitempty"`
	IsDir          bool           `json:"is_dir,omitempty"`
	Mode           uint32         `json:"mode,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
}

func (h *handlers) listArchiveContentsHandler(ctx context.Context, _ *mcp.CallToolRequest, in ListArchiveContentsInput) (*mcp.CallToolResult, ListArchiveContentsOutput, error) {
	path, err := expandHomeDir(in.Path)
	if err != nil {
		return nil, ListArchiveContentsOutput{}, fmt.Errorf("expand path: %w", err)
	}
	if path == "" {
		return nil, ListArchiveContentsOutput{}, errors.New("path is required")
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	result, err := search.WalkArchiveEntries(ctx, path, search.ArchiveWalkOptions{
		Expr:              in.Expr,
		Glob:              in.Glob,
		IncludeAttributes: in.IncludeAttributes,
		IncludeBody:       in.IncludeBody,
		EntryReadCap:      in.EntryReadCap,
		MaxEntries:        in.MaxEntries,
		Index:             h.idx,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, ListArchiveContentsOutput{}, fmt.Errorf("list_archive_contents: %w", err)
	}

	out := ListArchiveContentsOutput{ElapsedSeconds: elapsed}
	if result != nil {
		out.ScannedEntries = result.ScannedEntries
		out.MatchedEntries = result.MatchedEntries
		out.Truncated = result.Truncated
		out.CacheHit = result.CacheHit
		out.Cancelled = result.Cancelled
		out.CancellationReason = result.CancellationReason
		out.Entries = make([]ArchiveEntryWire, len(result.Entries))
		for i, e := range result.Entries {
			wire := ArchiveEntryWire{
				ArchivePath:    e.ArchivePath,
				Name:           e.Name,
				DisplayPath:    e.DisplayPath,
				Size:           e.Size,
				CompressedSize: e.CompressedSize,
				IsDir:          e.IsDir,
				Mode:           e.Mode,
				ContentType:    e.ContentType,
				Attributes:     e.Attributes,
			}
			if !e.ModTime.IsZero() {
				wire.ModTime = e.ModTime.Format(time.RFC3339)
			}
			out.Entries[i] = wire
		}
	}
	return nil, out, nil
}

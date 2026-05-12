// Package mcpserver exposes file-search-on as a Model Context Protocol server.
//
// The server has one file per MCP tool: search_tool.go, read_attributes_tool.go,
// read_lines_tool.go, find_duplicates_tool.go, stats_tool.go, index_stats_tool.go,
// list_attributes_tool.go. Each holds the tool's input/output structs and handler.
// This file (server.go) is just the wiring — handlers struct, instructions text,
// and the New/Run constructors.
package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// handlers wraps tool handlers so they can share an index reference
// and the server-level default timeout across the server's lifetime.
// The MCP SDK requires plain functions for AddTool, so we use closures
// to inject this shared state.
type handlers struct {
	idx            index.Index
	defaultTimeout time.Duration
}

// resolveTimeout returns a child ctx bounded by the effective per-call
// timeout, plus a cancel func the caller must defer. Precedence:
// timeoutSeconds (positive) > h.defaultTimeout (positive) > none.
// Passing &v with v <= 0 disables the timeout for this call (the
// parent ctx still applies). Tools without a per-call override (e.g.
// read_attributes, read_lines) pass nil to fall through to the server
// default. When the resolved timeout is <= 0 the original ctx and a
// no-op cancel are returned.
func (h *handlers) resolveTimeout(ctx context.Context, timeoutSeconds *float64) (context.Context, context.CancelFunc) {
	timeout := h.defaultTimeout
	if timeoutSeconds != nil {
		timeout = time.Duration(*timeoutSeconds * float64(time.Second))
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// serverInstructions is the text sent to MCP clients during initialize
// (via ServerOptions.Instructions). Clients like Claude Code surface
// this as system context, so the agent knows the predicate vocabulary
// without having to call list_attributes first. Keep it dense but
// scan-friendly: a paragraph of intent, the boolean predicate list, the
// common-attribute list, and a handful of CEL recipes covering the main
// content families.
const serverInstructions = `file-search-on is a content-type-aware file search. The 'search' tool takes a CEL expression evaluated over per-file attributes and returns matching paths plus structured metadata.

Use these boolean type predicates directly in your CEL expression — no need to call list_attributes first for them:

  is_markdown   .md, .markdown
  is_pdf        .pdf
  is_html       .html, .htm
  is_xml        .xml
  is_json       .json
  is_csv        .csv, .tsv
  is_text       plain text and log files
  is_image      .jpg, .jpeg, .png, .gif, .tif, .tiff, .heic, .webp
  is_audio      .mp3, .m4a, .flac, .ogg, .wav
  is_video      .mp4, .mov, .m4v, .mkv, .webm, .avi
  is_office     .docx, .xlsx, .pptx, .odt
  is_epub       .epub
  is_archive    .zip, .tar, .tar.gz, .gz
  is_binary     ELF / Mach-O / PE compiled binaries
  is_email      .eml, .mbox
  is_source     Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig

Common attributes available on every file: name, path, dir, ext, size (bytes, int), content_type. Per-family attributes the parser populates when the file matches:

  documents:  title, author, language, word_count, line_count, page_count
  data:       json_kind ("object"/"array"), csv_columns (list<string>), root_element
  markdown:   tags, categories, draft, date, frontmatter (map<string,dyn>), frontmatter_format
  images:     img_width, img_height, camera_make, camera_model, lens, taken_at, iso, focal_length, f_stop, exposure_time, gps_lat, gps_lon, orientation
  audio:      artist, album, album_artist, composer, year, track, genre, duration, bitrate, sample_rate, channels, bit_depth
  video:      video_codec, audio_codec, video_width, video_height, frame_rate, duration, is_hdr, subtitles
  archives:   entry_count, uncompressed_size, top_level_entries, has_root_dir
  binaries:   architectures (list<string>), bitness, binary_format, binary_type, is_dynamically_linked, is_stripped, entry_point
  email:      email_to, email_cc, email_message_id, email_in_reply_to, sent_at, attachment_count, email_count
  source:     language, line_count, loc, comment_loc, blank_loc

Recipe expressions:

  is_markdown && word_count > 500
  is_pdf && page_count > 10
  is_image && camera_make == "SONY" && iso > 1600
  is_image && taken_at > timestamp("2024-01-01T00:00:00Z")
  is_audio && sample_rate >= 96000
  is_video && video_height >= 2160 && duration > 1800
  is_csv && csv_columns.exists(c, c == "revenue")
  is_office && language == "fr"
  is_archive && uncompressed_size > 100000000
  is_binary && "x86_64" in architectures
  is_source && language == "go" && loc > 200
  is_email && size > 0 && sent_at > timestamp("2025-01-01T00:00:00Z")
  size > 10000000 && !is_video                                  // large non-video files
  is_markdown && tags.exists(t, t == "draft") && !draft
  levenshtein(artist, "Radiohead") <= 2 && is_audio             // fuzzy: typo-tolerant
  soundex(author) == soundex("Smith") && is_markdown            // phonetic
  point_in_polygon(gps_lat, gps_lon, [[51.5,-0.2],[51.6,-0.2],[51.6,0.0],[51.5,0.0]])  // images inside London bbox

Tools:
  search           run a CEL expression against a directory; returns matches[] and count
  read_attributes  same Match shape for one path; use when you already have the file
  read_lines       print a specific line range from a file — for context around a search match
  stats            histogram + totals for a directory tree, bucketed by any attribute via group_by
  find_duplicates  groups of byte-identical files keyed by sha256 — "what's eating my disk?"
  list_attributes  full schema (every attribute, every built-in function); call when the recipes above don't cover what you need
  index_stats      cache hit/miss counters for this server process

Performance: an attribute cache lives for the server's lifetime; repeated calls against the same files skip the per-file parse step. Empty 'expr' matches all files; empty 'dir' defaults to '.'.

Top-K and pagination: pass 'sort_by' to order results by an attribute, and 'limit' to cap the response. Recognised sort keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. 'order' is 'asc' (default) or 'desc'. Example for "10 most recent photos": {"expr": "is_image", "dir": "~/Pictures", "sort_by": "taken_at", "order": "desc", "limit": 10}. Without sort_by, limit returns the first N in walk order. With sort_by, the full result set is sorted then truncated to the top-K.

Snippets: pass 'include_snippet': true to populate each match's 'snippet' field with the first N lines of the file body (controlled by snippet_lines, default 10). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty. Useful for "show me what these files are about" without a follow-up read.

Body-content filters: pass 'include_body': true to expose the full file body to the CEL expression as the 'body' string variable. CEL's built-in string methods then act as content filters — body.contains("transformer"), body.matches("\\bAPI\\b") (RE2 regex), body.startsWith("Once upon"), size(body) > 5000. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB). EXPENSIVE — reads every candidate file, not just headers. Pair with a tight expr (e.g. 'is_markdown && body.contains(...)') so the type predicate prunes most candidates before the body read. Note: CEL's 'matches' uses RE2 (Google's regex syntax), the same engine Go's regexp/re2 package uses.

Stats / reconnaissance: the 'stats' tool aggregates a histogram + total counts + total sizes for a directory tree, optionally scoped by a CEL expr. Default bucket is content_type; pass 'group_by' to bucket by another attribute — ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Example: {expr:'is_image', group_by:'camera_make'} for photos-by-camera. Output's groups[] is the resolved histogram; content_types[] is populated alongside only for the default group_by (back-compat with v0.20 clients). Same excludes / respect_gitignore / timeout_seconds semantics as search; returns cancelled=true on timeout with the partial histogram intact.

Multi-directory search: both 'search' and 'stats' accept 'dirs': []string. When non-empty it overrides 'dir' and walks all roots in one call (each root's .gitignore is honoured independently). Useful when an agent needs to search across, say, ~/Documents AND ~/Downloads without two round-trips.

Read line ranges: the 'read_lines' tool returns lines [start_line, end_line] of a single file (1-indexed, inclusive). Useful as the second step after search — find matches via search, then call read_lines for context around each match without a separate read tool. max_lines caps the response (default 1000); the truncated flag tells you when the cap was hit.

Duplicate detection: 'find_duplicates' returns groups of byte-identical files keyed by sha256. Useful for 'what's eating my disk?' and 'find redundant copies' workflows. Two-pass for performance: files with unique sizes are skipped entirely (cheaper than computing their hash). Pair with expr to scope (e.g. expr='is_image' for photo dedup) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime) — first run on a large tree can be slow (every candidate file is read in full), but subsequent runs are free for unchanged files. Output: duplicates[] sorted by wasted_bytes descending — biggest reclamation candidates first.

Time-bucket aggregation: 'stats' group_by accepts mtime_year, mtime_month, mtime_day, taken_at_year/month/day, sent_at_year/month/day, and date_year/month/day in addition to the string-attribute keys. Files with zero timestamps bucket as "(no date)" so they don't collide with "1970-01-01". Example: {expr:'is_image', group_by:'taken_at_year'} for "photos per year".

Excluding directories: pass 'excludes' to skip directories and files by basename glob. Common values: ['node_modules', '.git', 'target', 'dist', '__pycache__', '*.bak']. Matched directories are pruned (their entire subtree is skipped). For path-aware semantics like 'src/build', set 'respect_gitignore': true and the server will parse a .gitignore at the walk root.

Timeouts and partial results: every tool call is wrapped with a server-default timeout (typically 60s; configured at server startup via --timeout). The 'search' tool also accepts 'timeout_seconds' on input — pass a positive number to override, or 0 to disable for that call. On expiry, the search tool DOES NOT return an error; it returns the partial match set with cancelled=true, cancellation_reason="timeout" (or "client_cancel" for transport-side cancellation), and elapsed_seconds set. Always inspect 'cancelled' in the response — a partial result set may be exactly what you want, or you may want to retry with a tighter expression / larger timeout / smaller dir. read_attributes is bounded by the same default timeout but returns an error on cancellation (no partial-result semantics for one file).`

// New builds an MCP server with file-search-on's tools registered. The
// server is not connected to a transport; callers either pass it to
// (*mcp.Server).Run for stdio service or (*mcp.Server).Connect for
// in-memory tests.
//
// idx is the attribute cache used by every search and read_attributes
// call. It is intended to be created once per server process and live
// for the server's lifetime — e.g. an in-memory index for stdio MCP,
// or a bbolt-backed index opened via index.Open(path) when the user
// wants persistence across restarts. nil idx disables caching; callers
// almost always want index.NewMemory() at the very least.
//
// defaultTimeout is the per-call ceiling applied to every tool
// invocation. <= 0 disables the default (calls inherit the parent ctx
// only). The search tool's input also accepts a per-call
// timeout_seconds override. A bounded default is strongly recommended
// because MCP clients have their own read deadlines; a runaway server
// walk would otherwise wedge the client.
func New(version string, idx index.Index, defaultTimeout time.Duration) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "file-search-on",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	h := &handlers{idx: idx, defaultTimeout: defaultTimeout}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Recursively search a directory for files matching a CEL expression evaluated over file metadata and content-type-specific attributes. Boolean type predicates you can use directly in expr: is_markdown, is_pdf, is_html, is_xml, is_json, is_csv, is_text, is_image, is_audio, is_video, is_office, is_epub, is_archive, is_binary, is_email, is_source. Common scalar attributes: size (int bytes), name, path, dir, ext, content_type, title, author, language, word_count, line_count, page_count. Per-family attributes (image EXIF, audio tags, video codec, frontmatter, archive sizes, binary architectures, email headers, source-LOC) populate when the matching family fires — see list_attributes for the full schema. Built-in functions: levenshtein(a, b), soundex(s), ngrams(s, n), ngram_similarity(a, b, n) for fuzzy / phonetic matching; point_in_polygon(lat, lon, polygon) for GPS-bbox filtering. Example exprs: 'is_markdown && word_count > 500'; 'is_pdf && page_count > 10'; 'is_image && iso > 1600'; 'is_video && video_height >= 2160 && duration > 1800'; 'is_source && language == \"go\" && loc > 200'. Top-K queries: pass sort_by + limit, e.g. {expr:'is_video', sort_by:'duration', order:'desc', limit:5} for the 5 longest videos. Sort keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. Snippets: pass include_snippet=true to populate match.snippet with the first N lines of body text (text content types only). Body filters: pass include_body=true to expose the full file body as the CEL 'body' variable, then use built-in string methods to filter: body.contains(\"X\"), body.matches(\"\\\\bX\\\\b\") (RE2 regex). Body reads every candidate file — pair with a tight type predicate (e.g. is_markdown). Exclusions: pass excludes (basename globs like ['node_modules', '.git', '*.bak']) and/or respect_gitignore=true to prune the walk. Repeated searches reuse a per-process attribute cache so unchanged files skip the parse step (see index_stats). Timeouts: every call is bounded by the server's default timeout (set at startup via --timeout, typically 60s); pass timeout_seconds in the input to override (positive = seconds, 0 = no timeout). On timeout the call DOES NOT error — it returns the partial match set with cancelled=true, cancellation_reason set, and elapsed_seconds populated. Always check 'cancelled' before treating the result as exhaustive.",
	}, h.searchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attributes",
		Description: "List every CEL attribute available to the search tool, the built-in functions (levenshtein, soundex, ngrams, ngram_similarity, point_in_polygon) with their signatures, and the registered content types.",
	}, listAttributesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_attributes",
		Description: "Extract content-type-specific attributes for a single file path. Use when the agent already knows the path and wants metadata without running a CEL filter or walking a directory. Returns the same Match shape as the search tool — title, author, EXIF, audio tags, video codec, frontmatter, etc., depending on the detected content type.",
	}, h.readAttributesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "index_stats",
		Description: "Return cumulative attribute-cache counters (hits, misses, puts, stales, errors) for the running MCP server. Counters reset on server restart.",
	}, h.indexStatsHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Description: "Aggregate counts and total sizes for a directory tree, bucketed by an attribute. Default bucket is content_type; pass group_by to bucket by ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, or frontmatter_format. Useful for 'what's in this folder?' and 'how many photos per camera?' style reconnaissance without retrieving individual paths. Accepts an optional CEL expr to scope the histogram (e.g. expr='is_image' + group_by='camera_make' for photos-by-camera). Multi-dir: pass 'dirs' to aggregate across multiple roots in one call. Honours the same excludes / respect_gitignore / timeout_seconds semantics as the search tool, including partial-result returns on cancellation. Output's `groups[]` is the histogram keyed by the resolved group_by; `content_types[]` is populated alongside only for the default group_by, kept for back-compat with older clients.",
	}, h.statsHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_lines",
		Description: "Print a specific line range from a single file. Completes the search-then-inspect loop without a separate read tool — agent flow: search for matches, then call read_lines for context around each match. Inputs: path (required), start_line (1-indexed inclusive; default 1), end_line (1-indexed inclusive; 0 = end of file), max_lines (cap; default 1000). Returns lines[] (no trailing newlines), total_lines, and truncated:true when the requested range exceeds max_lines. Errors only on missing/unreadable files or invalid ranges (start_line > end_line); pathological lines (huge / non-UTF-8) are truncated at 64 KiB per line and the scan continues.",
	}, h.readLinesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_duplicates",
		Description: "Find groups of byte-identical files keyed by sha256. Useful for 'what's eating my disk?' and 'find redundant copies' workflows. Two-pass for performance: files with unique sizes are skipped entirely (cheaper than computing their hash). Pair with expr to scope (e.g. expr='is_image' for photo dedup) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime) — first run on a large tree can be slow (every candidate file is read in full), but subsequent runs are free for unchanged files. Output: duplicates[] sorted by wasted_bytes descending — biggest reclamation candidates first.",
	}, h.findDuplicatesHandler)

	return s
}

// Run starts an MCP server on stdio with the given index and default
// per-call timeout, and blocks until the transport closes or ctx is
// cancelled.
func Run(ctx context.Context, version string, idx index.Index, defaultTimeout time.Duration) error {
	return New(version, idx, defaultTimeout).Run(ctx, &mcp.StdioTransport{})
}

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

	"github.com/richardwooding/gitmeta"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/monitor"
	"github.com/richardwooding/file-search-on/internal/search"
)

// handlers wraps tool handlers so they can share an index reference
// and the server-level default timeout across the server's lifetime.
// The MCP SDK requires plain functions for AddTool, so we use closures
// to inject this shared state.
type handlers struct {
	idx                    index.Index
	defaultTimeout         time.Duration
	defaultEmbeddingServer string
	defaultEmbeddingModel  string
	version                string
	// metrics, when non-nil, records per-tool-call telemetry for the
	// monitoring dashboard. nil disables instrumentation entirely.
	metrics *monitor.Collector
	// monitorCtl, when non-nil, owns the monitoring dashboard's lazy
	// lifecycle so the monitor_info tool can report its URL + peers and
	// (optionally) start it on demand. nil when monitoring isn't wired
	// (e.g. tests, or transports that never attach it).
	monitorCtl *monitor.Controller
	// sandbox, when non-empty, is the canonical absolute-path list of
	// allowed roots for every path-accepting tool input. Empty means
	// unrestricted (today's behaviour). Wired via WithSandbox.
	sandbox []string
	// gitPool caches *gitmeta.Cache instances across with_git=true
	// search calls. Initialised unconditionally in New() (cheap — just
	// allocates an empty map) and threaded into search.Options on
	// every walk. The pool re-validates HEAD on every Get so commits
	// between calls invalidate naturally. Primed at startup by the
	// MCP command's --warm goroutine. Issue #271 follow-up.
	gitPool *gitmeta.Pool
	// indexWatch, when non-nil, holds the counters for the background
	// index maintainer (the mcp --watch-index goroutine). Surfaced by
	// the index_stats tool. nil when no watcher is wired (tests, or a
	// server started without --watch-index) → reported as zeros.
	indexWatch *search.IndexWatchStats
	// semIndex is the warm in-memory vector index (issue #335 part 2).
	// search_semantic queries it instead of re-walking the tree once a
	// (dir, model) pair has been covered by a full walk. Always non-nil
	// (initialised in New); backed by the same attribute cache as idx.
	semIndex *semanticIndex
}

// Option configures the MCP server at construction. Used to attach
// optional cross-cutting state (e.g. the monitoring collector) without
// churning the New / Run signatures for the common case.
type Option func(*handlers)

// WithCollector attaches a monitoring collector so every tool call is
// timed and recorded. Pass the same collector to monitor.NewServer so
// the dashboard can read it back.
func WithCollector(c *monitor.Collector) Option {
	return func(h *handlers) { h.metrics = c }
}

// WithMonitor attaches the monitoring dashboard controller so the
// monitor_info tool can report the dashboard URL + peer instances and
// (when asked) start the dashboard on demand.
func WithMonitor(ctl *monitor.Controller) Option {
	return func(h *handlers) { h.monitorCtl = ctl }
}

// WithGitPool replaces the server's default in-process gitmeta.Pool
// with one supplied by the caller. The MCP command uses this to keep
// a reference to the pool so it can prime it via Warm() from the
// --warm startup goroutine. Passing nil is a no-op (the default
// pool initialised in New stays in place).
func WithGitPool(p *gitmeta.Pool) Option {
	return func(h *handlers) {
		if p != nil {
			h.gitPool = p
		}
	}
}

// WithIndexWatchStats attaches the background index maintainer's
// counters so the index_stats tool can report watch_refreshed /
// watch_evicted / watch_errors. Passing nil is a no-op; the tool then
// reports zeros for those fields.
func WithIndexWatchStats(s *search.IndexWatchStats) Option {
	return func(h *handlers) {
		if s != nil {
			h.indexWatch = s
		}
	}
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
  is_json       .json (also fires for package.json / package-lock.json)
  is_yaml       .yaml, .yml
  is_toml       .toml (also fires for Cargo.toml / Cargo.lock)
  is_csv        .csv, .tsv
  is_text       plain text and log files (also fires for requirements.txt, LICENSE, CHANGELOG, CONTRIBUTING)
  is_image      .jpg, .jpeg, .png, .gif, .tif, .tiff, .heic, .webp
  is_audio      .mp3, .m4a, .flac, .ogg, .wav
  is_video      .mp4, .mov, .m4v, .mkv, .webm, .avi
  is_office     .docx, .xlsx, .pptx, .odt
  is_epub       .epub
  is_archive    .zip, .tar, .tar.gz, .gz
  is_binary     ELF / Mach-O / PE compiled binaries
  is_email      .eml, .mbox
  is_source     Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Scala / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig / C# / PHP / Perl / R / Ada / SQL / Visual Basic / Fortran / MATLAB / Assembly / Pascal
  is_disk_image .dmg, .iso, .vhd, .vhdx, .vmdk, .qcow2, .qcow, .wim — umbrella across all disk-image formats
  is_dmg        .dmg (Apple Disk Image / UDIF)
  is_iso        .iso (ISO 9660 CD/DVD image)
  is_vhd        .vhd (Microsoft Virtual Hard Disk legacy)
  is_vhdx       .vhdx (Microsoft Virtual Hard Disk v2)
  is_vmdk       .vmdk (VMware sparse-extent — descriptor-only text .vmdk falls through to text)
  is_qcow2      .qcow2, .qcow (QEMU Copy-On-Write v2/v3)
  is_wim        .wim (Windows Imaging Format)
  is_install_package .pkg, .deb, .rpm, .appimage — umbrella across install-package formats
  is_pkg        .pkg (macOS installer, XAR archive)
  is_deb        .deb (Debian binary package, ar archive)
  is_rpm        .rpm (Red Hat Package Manager — surfaces package_name/version/release/arch from the legacy Lead)
  is_appimage   .appimage (Linux portable ELF + appended SquashFS)
  is_test_file  source/* file whose basename matches its language's test convention (*_test.go for Go, test_*.py / *_test.py for Python, *.test.{js,ts,tsx} / *.spec.{...} for JS/TS, *Test.java / *Tests.java / *IT.java for Java, *_spec.rb / *_test.rb for Ruby, *Tests.swift / *Test.kt / *Spec.scala for Swift/Kotlin/Scala, *Test.cs / *Tests.cs for C#, tests/*.rs for Rust integration tests)
  is_bytecode   .class (Java), .pyc / .pyo (Python), .wasm (WebAssembly) — umbrella across all VM bytecode formats
  is_class      Java .class file (CAFEBABE magic) — surfaces class_name, super_class, interfaces, method_count, field_count, access_flags
  is_pyc        Python compiled bytecode (.pyc / .pyo) — surfaces python_version, source_mtime
  is_wasm       WebAssembly module (.wasm) — surfaces wasm_version, section_count, import_count, export_count
  is_symlink         entry is a symbolic link (decided by os.Lstat). For symlinks-to-files the other attributes reflect the TARGET (size, content_type, etc.); for symlinks-to-dirs they reflect the symlink itself (treated as a leaf). target_path carries the raw link target.
  is_broken_symlink  is_symlink AND the target can't be resolved (dangling link). content_type is empty in this case; size / mod_time come from the symlink's own Lstat info.

Exact-name content types (matched by filename, not extension). Both the
per-type predicate AND the family predicate fire for each match. When
the file is also a recognised underlying format (JSON / TOML / plain
text), that predicate fires too — so an agent can write either the
specific predicate or the broader format predicate and get the same
matches:

  is_dockerfile / is_build    Dockerfile, Containerfile
  is_makefile / is_build      Makefile (+ variants), GNUmakefile, BSDmakefile
  is_justfile / is_build      Justfile, justfile
  is_rakefile / is_build      Rakefile
  is_license / is_repo_meta / is_text       LICENSE, LICENCE, COPYING (bare; LICENSE.md is markdown)
  is_changelog / is_repo_meta / is_text     CHANGELOG, HISTORY (bare)
  is_contributing / is_repo_meta / is_text  CONTRIBUTING (bare)
  is_codeowners / is_repo_meta    CODEOWNERS, OWNERS
  is_gitignore / is_ignore    .gitignore, .gitattributes
  is_dockerignore / is_ignore .dockerignore
  is_gomod / is_manifest      go.mod, go.sum
  is_node_manifest / is_manifest / is_json     package.json, package-lock.json
  is_cargo_manifest / is_manifest / is_toml    Cargo.toml, Cargo.lock
  is_pipfile / is_manifest                     Pipfile, Pipfile.lock
  is_python_reqs / is_manifest / is_text       requirements.txt
  is_gemfile / is_manifest    Gemfile, Gemfile.lock
  is_procfile / is_platform   Procfile
  is_vagrantfile / is_platform      Vagrantfile
  is_ds_store / is_macos_metadata / is_system_metadata        .DS_Store (macOS Finder window state)
  is_localized / is_macos_metadata / is_system_metadata       .localized (macOS Finder localization marker)
  is_thumbs_db / is_windows_metadata / is_system_metadata     Thumbs.db, ehthumbs.db, ehthumbs_vista.db (Windows thumbnail cache)
  is_desktop_ini / is_windows_metadata / is_system_metadata   Desktop.ini (Windows folder customisation)
  is_kde_directory / is_linux_metadata / is_system_metadata   .directory (KDE Dolphin folder properties)

Common attributes available on every file: name, path, dir, ext, size (bytes, int), content_type. Per-family attributes the parser populates when the file matches:

  documents:  title, author, language, word_count, line_count, page_count
  data:       json_kind ("object"/"array"), yaml_kind ("object"/"array"/"scalar"), yaml_document_count, csv_columns (list<string>), root_element
  manifest:   module, go_version (go.mod)
  build:      base_image (Dockerfile FROM directive)
  markdown:   tags, categories, draft, date, frontmatter (map<string,dyn>), frontmatter_format
  images:     img_width, img_height, camera_make, camera_model, lens, taken_at, iso, focal_length, f_stop, exposure_time, gps_lat, gps_lon, orientation
  audio:      artist, album, album_artist, composer, year, track, genre, duration, bitrate, sample_rate, channels, bit_depth
  video:      video_codec, audio_codec, video_width, video_height, frame_rate, duration, is_hdr, subtitles
  archives:   entry_count, uncompressed_size, top_level_entries, has_root_dir
  binaries:   architectures (list<string>), bitness, binary_format, binary_type, is_dynamically_linked, is_stripped, entry_point
  email:      email_to, email_cc, email_message_id, email_in_reply_to, sent_at, attachment_count, email_count
  source:     language, line_count, loc, comment_loc, blank_loc
  disk-image: disk_image_format, virtual_size, disk_type (VHD/VMDK), volume_label (ISO), disk_image_created_at (VHD/ISO; in-header creation time), cluster_bits (QCOW2), is_encrypted (QCOW2), image_count (WIM)
  install:    package_format, package_name (RPM), package_version (RPM), package_release (RPM), package_arch (RPM), package_kind, appimage_version
  repo-meta:  license_id (SPDX id detected from LICENSE / LICENCE / COPYING / UNLICENSE body)
  bytecode:   bytecode_format, runtime_version, class_name (JVM), super_class (JVM), interfaces (JVM), method_count (JVM), field_count (JVM), access_flags (JVM), python_version, source_mtime, wasm_version, section_count, import_count, export_count

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
  find_near_duplicates  groups of SIMILAR files via SimHash fingerprint — catches typo edits, regenerated headers, template copies that find_duplicates misses
  list_archive_contents  list or filter entries inside a ZIP / TAR / TAR.GZ / GZIP archive without extracting — full CEL vocabulary on per-entry attributes
  read_file_in_archive   read a single named file's bytes out of an archive — useful for "pull pyproject.toml from source.tar.gz"
  find_matches     scan text files for a regex; returns line-level hits with context — "find references to X"
  detect_project   what kind of project (go / node / rust / python / …) is THIS directory
  find_projects    walk a root and identify every project subdirectory under it
  resolve_project_for_path  given an arbitrary path, walk up and return the nearest project root + types
  list_attributes  full schema (every attribute, every built-in function); call when the recipes above don't cover what you need
  index_stats      cache hit/miss counters for this server process

Performance: an attribute cache lives for the server's lifetime; repeated calls against the same files skip the per-file parse step. Empty 'expr' matches all files; empty 'dir' defaults to '.'.

Top-K and pagination: pass 'sort_by' to order results by an attribute, and 'limit' to cap the response. Recognised sort keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. 'order' is 'asc' (default) or 'desc'. Example for "10 most recent photos": {"expr": "is_image", "dir": "~/Pictures", "sort_by": "taken_at", "order": "desc", "limit": 10}. Without sort_by, limit returns the first N in walk order. With sort_by, the full result set is sorted then truncated to the top-K.

Snippets: pass 'include_snippet': true to populate each match's 'snippet' field with the first N lines of the file body (controlled by snippet_lines, default 10). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty. Useful for "show me what these files are about" without a follow-up read.

Body-content filters: pass 'include_body': true to expose the full file body to the CEL expression as the 'body' string variable. CEL's built-in string methods then act as content filters — body.contains("transformer"), body.matches("\\bAPI\\b") (RE2 regex), body.startsWith("Once upon"), size(body) > 5000. Text-shaped types (markdown / text / html / csv / json / xml / source/*) populate via a raw byte read; structured-document types (office/docx / office/xlsx / office/pptx / office/odt / epub / email/rfc822 / email/mbox / pdf) populate via a format-specific extractor. Office and EPUB extractors walk the ZIP envelope and pull plain text out of the content XML (word/document.xml, sharedStrings.xml + inline-string cells, ppt/slides/slideN.xml, content.xml, OPF spine → (X)HTML chapters). Email extractors walk the MIME tree: multipart/alternative prefers text/plain over text/html; multipart/mixed / related concatenates every non-attachment text part; Content-Transfer-Encoding (quoted-printable / base64) is decoded; mbox archives concatenate every message's body so body.contains("invoice") searches an entire inbox in one call. PDF extractor walks pages linearly, decoding glyphs against pre-cached page fonts / ToUnicode CMaps; image-only / scanned PDFs and encrypted PDFs surface as empty body (detect with 'is_pdf && size(body) == 0' to find OCR candidates), and '(cid:N)' glyph fallbacks are stripped automatically. Headers (Subject / From / etc.) are NOT in the body — they surface as separate CEL variables (title / author / email_to / sent_at). Attachments are skipped. So body.contains("Q3 revenue") works on a spreadsheet, a PDF, an EPUB chapter, an .eml, or an mbox the same way it works on a markdown file. Capped at body_max_bytes (default 1 MiB, applied to EXTRACTED text — not raw file size — so a 50 MB mbox with sparse text reads cheaply). EXPENSIVE — reads every candidate file. Pair with a tight expr (e.g. 'is_pdf && body.contains(...)') so the type predicate prunes most candidates before the body read. Note: CEL's 'matches' uses RE2 (Google's regex syntax), the same engine Go's regexp/re2 package uses.

Stats / reconnaissance: the 'stats' tool aggregates a histogram + total counts + total sizes for a directory tree, optionally scoped by a CEL expr. Default bucket is content_type; pass 'group_by' to bucket by another attribute — ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Example: {expr:'is_image', group_by:'camera_make'} for photos-by-camera. Output's groups[] is the resolved histogram; content_types[] is populated alongside only for the default group_by (back-compat with v0.20 clients). Same excludes / respect_gitignore / timeout_seconds semantics as search; returns cancelled=true on timeout with the partial histogram intact.

Multi-directory search: both 'search' and 'stats' accept 'dirs': []string. When non-empty it overrides 'dir' and walks all roots in one call (each root's .gitignore is honoured independently). Useful when an agent needs to search across, say, ~/Documents AND ~/Downloads without two round-trips.

Path expansion: every path-shaped input (dir, dirs, path) is tilde-expanded at tool entry. A leading "~/" (or a bare "~") resolves to the user's home directory before any filesystem operation runs. So {"dir": "~/Code"} works the same as it would in a shell — no need to pre-expand. The "~user/" form (someone else's home dir) is NOT expanded; pass an absolute path for that case.

Read line ranges: the 'read_lines' tool returns lines [start_line, end_line] of a single file (1-indexed, inclusive). Useful as the second step after search — find matches via search, then call read_lines for context around each match without a separate read tool. max_lines caps the response (default 1000); the truncated flag tells you when the cap was hit.

Duplicate detection: 'find_duplicates' returns groups of byte-identical files keyed by sha256. Useful for 'what's eating my disk?' and 'find redundant copies' workflows. Two-pass for performance: files with unique sizes are skipped entirely (cheaper than computing their hash). Pair with expr to scope (e.g. expr='is_image' for photo dedup) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime) — first run on a large tree can be slow (every candidate file is read in full), but subsequent runs are free for unchanged files. Output: duplicates[] sorted by wasted_bytes descending — biggest reclamation candidates first.

Near-duplicate detection: 'find_near_duplicates' returns groups of files whose bodies are SIMILAR (not identical) via Charikar SimHash. Complements find_duplicates for the "is this a fork of X?" / "did I save two versions of this note?" workflow — catches trailing-newline edits, regenerated headers, typo fixes, template copies. Configurable similarity threshold (default 0.85 ≈ 9-bit Hamming distance on a 64-bit fingerprint; raise to 0.95 for near-byte-identical, lower to 0.75 for significant overlap). Fingerprints only fire for text-shaped (markdown / text / html / csv / json / xml / source/*) and structured-document (pdf / office / epub / email) types — binary families return zero fingerprints and are excluded. Fingerprints cache alongside the per-file hash; repeat runs on unchanged trees skip body extraction AND SimHash compute. Output: groups[] sorted by member count desc; each group has a representative (largest file in the group), a fingerprint (hex), and members[] sorted by similarity desc.

Time-bucket aggregation: 'stats' group_by accepts mtime_year, mtime_month, mtime_day, taken_at_year/month/day, sent_at_year/month/day, and date_year/month/day in addition to the string-attribute keys. Files with zero timestamps bucket as "(no date)" so they don't collide with "1970-01-01". Example: {expr:'is_image', group_by:'taken_at_year'} for "photos per year".

Excluding directories: pass 'excludes' to skip directories and files by basename glob. Common values: ['node_modules', '.git', 'target', 'dist', '__pycache__', '*.bak']. Matched directories are pruned (their entire subtree is skipped). For path-aware semantics like 'src/build', set 'respect_gitignore': true and the server will parse a .gitignore at the walk root.

Timeouts and partial results: every tool call is wrapped with a server-default timeout (typically 60s; configured at server startup via --timeout). The 'search' tool also accepts 'timeout_seconds' on input — pass a positive number to override, or 0 to disable for that call. On expiry, the search tool DOES NOT return an error; it returns the partial match set with cancelled=true, cancellation_reason="timeout" (or "client_cancel" for transport-side cancellation), and elapsed_seconds set. Always inspect 'cancelled' in the response — a partial result set may be exactly what you want, or you may want to retry with a tighter expression / larger timeout / smaller dir. read_attributes is bounded by the same default timeout but returns an error on cancellation (no partial-result semantics for one file).

Project detection: 18 built-in project types — go / node / rust / python / ruby / java-maven / java-gradle / dotnet / terraform / docker-compose, plus static-site generators hugo / jekyll / eleventy / astro / gatsby / mkdocs / docusaurus / pelican. The detect_project / find_projects / resolve_project_for_path tools surface these. With resolve_projects: true on search, files inherit a project_type (string), project_types (list), and is_static_site (bool — fires for any of the 8 SSGs) so you can filter by project context: 'is_source && project_type == "hugo"' or 'is_static_site && is_markdown && draft'. Build-artefact pruning understands each type's canonical output dir — pass prune_build_artefacts: true to skip vendor / node_modules / target / __pycache__ / public / _site / dist etc. automatically.

Field projection: 'search' and 'read_attributes' both accept a 'fields': []string input to project responses to only the listed attributes. Saves tokens when only a few attributes matter — e.g. {expr:'is_image', sort_by:'taken_at', order:'desc', limit:50, fields:['taken_at','camera_model']} returns 50 matches with just path / content_type / size / taken_at / camera_model. 'path', 'content_type', and 'size' are always included regardless. Sort still works on attributes not in 'fields' — sort happens before projection. Unknown names error at request validation; call 'list_attributes' for the canonical schema. Empty / omitted returns every populated attribute (existing default behaviour).`

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
// EmbedDefaults carries the optional server-startup defaults for the
// search_semantic tool. Empty values mean "no default; per-call inputs
// MUST supply this", except for Server which falls back to
// http://localhost:11434.
type EmbedDefaults struct {
	Server string // Ollama base URL; "" → http://localhost:11434
	Model  string // Ollama embedding model name; "" → no default
}

func New(version string, idx index.Index, defaultTimeout time.Duration, embedDefaults EmbedDefaults, opts ...Option) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "file-search-on",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	server := embedDefaults.Server
	if server == "" {
		server = "http://localhost:11434"
	}
	h := &handlers{
		idx:                    idx,
		defaultTimeout:         defaultTimeout,
		defaultEmbeddingServer: server,
		defaultEmbeddingModel:  embedDefaults.Model,
		version:                version,
		gitPool:                gitmeta.NewPool(),
		semIndex:               newSemanticIndex(idx),
	}
	for _, opt := range opts {
		opt(h)
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Recursively search a directory for files matching a CEL expression evaluated over file metadata and content-type-specific attributes. Boolean type predicates you can use directly in expr: is_markdown, is_pdf, is_html, is_xml, is_json, is_yaml, is_csv, is_text, is_image, is_audio, is_video, is_office, is_epub, is_archive, is_binary, is_email, is_source, is_disk_image (umbrella for is_dmg/is_iso/is_vhd/is_vhdx/is_vmdk/is_qcow2/is_wim), is_install_package (umbrella for is_pkg/is_deb/is_rpm/is_appimage), is_bytecode (umbrella for is_class/is_pyc/is_wasm — VM bytecode artefacts with parsed runtime_version + per-format metadata), is_test_file (source files matching the per-language test convention), is_symlink + is_broken_symlink (filesystem-level symlink awareness; target_path attribute carries the raw link target). Common scalar attributes: size (int bytes), name, path, dir, ext, content_type, title, author, language, word_count, line_count, page_count. Per-family attributes (image EXIF, audio tags, video codec, frontmatter, archive sizes, binary architectures, email headers, source-LOC, disk-image virtual_size / disk_image_format / volume_label / cluster_bits / is_encrypted / image_count, install-package package_format / package_name / package_version / package_arch / package_kind, license_id for repo-license files) populate when the matching family fires — see list_attributes for the full schema. Built-in functions: levenshtein(a, b), soundex(s), ngrams(s, n), ngram_similarity(a, b, n) for fuzzy / phonetic matching; point_in_polygon(lat, lon, polygon) for GPS-bbox filtering. Example exprs: 'is_markdown && word_count > 500'; 'is_pdf && page_count > 10'; 'is_image && iso > 1600'; 'is_video && video_height >= 2160 && duration > 1800'; 'is_source && language == \"go\" && loc > 200'. Top-K queries: pass sort_by + limit, e.g. {expr:'is_video', sort_by:'duration', order:'desc', limit:5} for the 5 longest videos. Sort keys: size, name, path, mod_time, created_at, metadata_changed_at, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count, virtual_size, image_count, disk_image_created_at, cluster_bits. Snippets: pass include_snippet=true to populate match.snippet with the first N lines of body text (text content types only). Body filters: pass include_body=true to expose the full file body as the CEL 'body' variable, then use built-in string methods to filter: body.contains(\"X\"), body.matches(\"\\\\bX\\\\b\") (RE2 regex). Body reads every candidate file — pair with a tight type predicate (e.g. is_markdown). Exclusions: pass excludes (basename globs like ['node_modules', '.git', '*.bak']) and/or respect_gitignore=true to prune the walk. Repeated searches reuse a per-process attribute cache so unchanged files skip the parse step (see index_stats). Timeouts: every call is bounded by the server's default timeout (set at startup via --timeout, typically 60s); pass timeout_seconds in the input to override (positive = seconds, 0 = no timeout). On timeout the call DOES NOT error — it returns the partial match set with cancelled=true, cancellation_reason set, and elapsed_seconds populated. Always check 'cancelled' before treating the result as exhaustive.",
	}, instrument(h.metrics, "search", h.searchHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_semantic",
		Description: "Semantic similarity search via local Ollama embeddings. Returns files RANKED by conceptual similarity to a natural-language query — paraphrase, synonyms, and topic-level matches surface even when the exact words don't appear in the body. Required input: 'query' (the natural-language search). Optional: 'threshold' (default 0.5; cosine similarity floor — 0.7+ for strict topical match, 0.4-0.5 for loose / related), 'limit' (default 50; top-K cap), 'expr' (CEL pre-filter using the same vocabulary as the search tool — e.g. 'is_pdf || is_office' to scope to documents), 'model' / 'embedding_server' (per-call overrides for the server-startup defaults — useful when you've pulled multiple embedding models). Setup: requires Ollama running locally (https://ollama.com) and one embedding model pulled (e.g. 'ollama pull nomic-embed-text'). The MCP server boots WITHOUT Ollama; the first search_semantic call performs the connection check and returns a clear error if Ollama is unreachable or the model isn't pulled. The per-file embedding is cached alongside (size, mtime) so repeat searches against an unchanged tree are I/O-cheap. After the first full walk of a (dir, model) pair, repeat queries are served from a warm in-memory vector index — no filesystem walk and no re-embedding — and the response sets ann_used=true (it falls back to a full walk, ann_used=false, when the directory's structure changes; content edits are detected per-file and reported via ann_stale_skipped). For hybrid keyword+semantic ranking pass hybrid=true (fuses BM25 keyword relevance with embedding similarity via reciprocal-rank fusion). Each hit pinpoints WHERE it matched: match_start_line / match_end_line give the line range of the best-matching chunk, and for source files (which are chunked by function span) match_symbol names the matching function/method — fetch the code with read_lines on [match_start_line, match_end_line], or pass include_match_snippet=true to inline that region as match_snippet on each hit (text/source files only; bounded by snippet_lines, default 60). Use 'list_embedding_models' to discover what's installed and 'pull_embedding_model' to install a recommended one.",
	}, instrument(h.metrics, "search_semantic", h.searchSemanticHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_embedding_models",
		Description: "List Ollama embedding models — both what's locally pulled on the configured Ollama server and a curated catalog of recommended models that aren't yet installed. Each locally-pulled entry is annotated with catalogued=true when it matches a known model in the curated list (plus its description and embedding dimensions). The catalog arm is the answer to 'what could I install?' — five well-known embedding models (nomic-embed-text, mxbai-embed-large, all-minilm, snowflake-arctic-embed, bge-m3) with one-line descriptions, approximate size, and emitted vector dimensions. Pass embedding_server to query a non-default Ollama (e.g. http://gpu-box:11434). Returns an error when Ollama is unreachable; the search_semantic tool's setup story relies on this tool's output to guide the user.",
	}, instrument(h.metrics, "list_embedding_models", h.listEmbeddingModelsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "pull_embedding_model",
		Description: "Download an Ollama embedding model on demand (synchronous; blocks until the pull completes). Equivalent to running `ollama pull <name>` against the configured Ollama server. Inputs: name (required, e.g. nomic-embed-text — omit the tag to pull :latest), embedding_server (optional override). Returns already_pulled=true with sub-second latency when the model is already installed (the safe to-retry shortcut). For a fresh pull of a ~270 MB model on a reasonable connection expect 30–120 seconds; for ~700 MB expect 1–5 minutes. The pull is layer-based and resumable inside Ollama — if the MCP per-call timeout fires before completion, Ollama keeps pulling server-side and the next call will return already_pulled=true once it finishes. Pair with list_embedding_models to discover the recommended catalog before pulling.",
	}, instrument(h.metrics, "pull_embedding_model", h.pullEmbeddingModelHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attributes",
		Description: "Schema discovery for the search tool. Three modes — default is a small summary that fits any MCP token budget; drill in via inputs. Pass no args for the summary (counts per section + every CEL function's name + a hint). Pass section='common' / 'type_specific' / 'frontmatter' / 'functions' / 'content_types' to fetch one section with optional limit+offset pagination (default 50, cap 500). Pass names=['loc','symbols','levenshtein',…] for direct lookup across every section — returns matching attribute / function / content-type entries from wherever they live. The legacy 'return the full schema' call would emit ~100kB of JSON and blow the per-response budget; #273 split it into the three drill-in paths above.",
	}, instrument(h.metrics, "list_attributes", h.listAttributesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "validate_expr",
		Description: "Validate a CEL expression without running a walk. Compiles via the same env the search tool uses (every declared variable + every built-in function in scope) and returns ok / error / referenced_variables / referenced_functions / suggestion. On compile failure, surfaces the cel-go error message AND (when the failure was an unknown identifier within Levenshtein distance 2 of a known name) a 'did you mean X?' suggestion. The referenced_* lists populate regardless of ok so the caller can correlate against list_attributes even when their typo blocks compilation. Cheap — no file IO, no walk. Use during iterative CEL refinement to avoid burning a search call on every typo. Issue #282.",
	}, instrument(h.metrics, "validate_expr", h.validateExprHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_attributes",
		Description: "Extract content-type-specific attributes for a single file path. Use when the agent already knows the path and wants metadata without running a CEL filter or walking a directory. Returns the same Match shape as the search tool — title, author, EXIF, audio tags, video codec, frontmatter, etc., depending on the detected content type.",
	}, instrument(h.metrics, "read_attributes", h.readAttributesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "index_stats",
		Description: "Return cumulative attribute-cache counters (hits, misses, puts, stales, errors) for the running MCP server. Counters reset on server restart.",
	}, instrument(h.metrics, "index_stats", h.indexStatsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Description: "Aggregate counts and total sizes for a directory tree, bucketed by an attribute. Default bucket is content_type; pass group_by to bucket by ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, or frontmatter_format. Useful for 'what's in this folder?' and 'how many photos per camera?' style reconnaissance without retrieving individual paths. Accepts an optional CEL expr to scope the histogram (e.g. expr='is_image' + group_by='camera_make' for photos-by-camera). Multi-dir: pass 'dirs' to aggregate across multiple roots in one call. Honours the same excludes / respect_gitignore / timeout_seconds semantics as the search tool, including partial-result returns on cancellation. Output's `groups[]` is the histogram keyed by the resolved group_by; `content_types[]` is populated alongside only for the default group_by, kept for back-compat with older clients.",
	}, instrument(h.metrics, "stats", h.statsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "churn_owners",
		Description: "Surface ownership concentration and bus-factor risk per directory — which subtrees are effectively single-maintainer. Walks with git metadata on, groups tracked files by their parent directory, and reports for each: distinct_authors, top_author + top_author_share (fraction of the dir's files that author last touched), files, total_commits. Ranked by bus-factor risk — fewest authors first, then highest churn — so single-author high-traffic directories sit at the top. Input: dir / dirs (default '.'), expr (default 'is_git_tracked'; narrow to 'is_source' for code-only), min_files (drop small dirs; default 1), plus the usual workers / excludes / prune_build_artefacts / timeout_seconds. Output: dirs[] {dir, files, distinct_authors, top_author, top_author_share, total_commits}, total_files. APPROXIMATE — keys on git_last_commit_author (the LAST committer per file), not a full blame, so it flags single-maintainer subtrees rather than computing true line-level ownership. Requires the walk root to be inside a git working tree.",
	}, instrument(h.metrics, "churn_owners", h.churnOwnersHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_lines",
		Description: "Print a specific line range from a single file. Completes the search-then-inspect loop without a separate read tool — agent flow: search for matches, then call read_lines for context around each match. Inputs: path (required), start_line (1-indexed inclusive; default 1), end_line (1-indexed inclusive; 0 = end of file), max_lines (cap; default 1000). Returns lines[] (no trailing newlines), total_lines, and truncated:true when the requested range exceeds max_lines. Errors only on missing/unreadable files or invalid ranges (start_line > end_line); pathological lines (huge / non-UTF-8) are truncated at 64 KiB per line and the scan continues.",
	}, instrument(h.metrics, "read_lines", h.readLinesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_duplicates",
		Description: "Find groups of byte-identical files keyed by sha256. Useful for 'what's eating my disk?' and 'find redundant copies' workflows. Two-pass for performance: files with unique sizes are skipped entirely (cheaper than computing their hash). Pair with expr to scope (e.g. expr='is_image' for photo dedup) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime) — first run on a large tree can be slow (every candidate file is read in full), but subsequent runs are free for unchanged files. Output: duplicates[] sorted by wasted_bytes descending — biggest reclamation candidates first.",
	}, instrument(h.metrics, "find_duplicates", h.findDuplicatesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_near_duplicates",
		Description: "Find groups of SIMILAR (not identical) files via SimHash fingerprint of their extracted body. Complements 'find_duplicates' for fuzzy matching — catches files with trailing-newline edits, regenerated headers, typo fixes, template copies, and minor revisions that exact-hash dedup misses. Algorithm: 64-bit Charikar SimHash over 3-word shingles of the body text → pairwise Hamming distance → union-find groups files within the similarity threshold. (Shingles, not single words: a bag-of-words hash is dominated by the near-universal English stopword distribution, so unrelated prose — or files sharing only a boilerplate header/footer — clustered spuriously; shingling keys on phrasing so unrelated prose drops near the 0.55 random baseline while genuine near-dups stay high.) Inputs: expr (optional CEL pre-prune; e.g. is_markdown to limit to docs), threshold (0..1, default 0.85; 0.95 for near-identical, 0.75 for looser structural overlap), min_size (skip tiny files), the usual dir / dirs / excludes / timeout_seconds. Only text-shaped and structured-document types fingerprint (markdown / text / html / csv / json / xml / source/* / pdf / office / epub / email); binary families return zero fingerprints and are excluded. Fingerprints cache in the attribute index alongside hash; repeat runs on unchanged trees skip body extraction AND SimHash compute. Output: groups[] sorted by member count desc — biggest near-duplicate clusters first. Similarity = 1.0 means identical body text (which would also surface via find_duplicates).",
	}, instrument(h.metrics, "find_near_duplicates", h.findNearDuplicatesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_duplicate_functions",
		Description: "Find clusters of near-identical FUNCTIONS across the tree — copy-pasted logic that the file-level 'find_near_duplicates' misses (a duplicated 20-line helper inside two otherwise-distinct 600-line files never trips a whole-file fingerprint). Splits each source file into its functions/methods (the same per-function spans 'complexity' and function-level semantic search use), SimHashes each function body, and union-find groups functions within the similarity threshold. Inputs: expr (optional CEL pre-filter; defaults to 'is_source'), threshold (0..1, default 0.92 — code SimHash sits high even for unrelated functions, so this is tighter than the prose default), min_lines (skip functions shorter than this; default 5 — trivial getters/wrappers SimHash alike and would bury real duplication), plus the usual dir / dirs / excludes / timeout_seconds. Source languages only (Go + the tree-sitter set); non-source files contribute no functions. Output: groups[] (member count desc, then total duplicated lines), each member {path, symbol, start_line, end_line, lines, similarity} — read_lines the span to see the code. Use to find extract-this-helper refactor opportunities. Heuristic: SimHash matches structure/tokens, so genuinely-different functions that share a skeleton can cluster — review before extracting. Grouping is O(N²) over scanned functions; the min_lines filter keeps that tractable.",
	}, instrument(h.metrics, "find_duplicate_functions", h.findDuplicateFunctionsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "diff_trees",
		Description: "Cross-tree set operations by sha256 content hash — the inverse of find_duplicates (which finds dupes WITHIN a tree). Answers 'what's in tree A that's NOT in tree B?', 'what content do they share?', 'which same-named files drifted?'. Inputs: tree_a + tree_b (required), op ('a-minus-b' default = content in A but not B, 'b-minus-a', 'intersect' = in both, 'union' = all distinct, 'mismatch' = same relative path but differing content), expr (optional CEL pre-prune applied to both trees, e.g. 'size > 1000000'), plus excludes / respect_gitignore / follow_symlinks / min_size / timeout_seconds. Hashes cache in the attribute index alongside (size, mtime) — two warm trees diff in seconds. Output: records[] of {status, path_a, path_b, sha256, size} sorted by (path_a, path_b, sha256); status ∈ only_in_a / only_in_b / both / name_match_content_differs. Read-only — never mutates either tree. Use cases: pre-backup sanity ('what's on the external drive I don't have locally?'), incremental-migration verification, sync-correctness checks, drift detection. Issue #210.",
	}, instrument(h.metrics, "diff_trees", h.diffTreesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_archive_contents",
		Description: "List or filter entries inside a ZIP / TAR / TAR.GZ / GZIP archive without extracting. Per-entry CEL evaluation against the SAME vocabulary the top-level search uses — every is_X predicate (is_source, is_dockerfile, is_pdf, …) and per-family attribute (loc, language, page_count, frontmatter, …) works inside archives. Detection runs on the entry's bytes (first 512 sniffed against a synthetic in-memory FS), so 'src/main.go' inside a tarball detects as source/go and surfaces loc / comment_loc just like a real file. Inputs: path (required), expr (optional CEL filter), glob (basename pattern applied BEFORE the CEL pass), include_attributes (off by default — terse listing of name/size/content_type only), include_body (read entry bodies so body.contains / body.matches fire; bypasses the entry-list cache), max_entries (cap), timeout_seconds. The entry-list cache uses the existing attribute index: hit path filters cached records by glob + expr without opening the archive; miss path walks + populates the cache asynchronously. Archives with > 10000 entries skip the cache (too large to encode). Output: entries[] sorted by walk order; cache_hit=true when the response came from cache. Use when an agent needs to answer 'does this tarball contain Dockerfile?' or 'find every Go file with loc > 200 inside any tarball' without extracting first.",
	}, instrument(h.metrics, "list_archive_contents", h.listArchiveContentsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_file_in_archive",
		Description: "Read a single named file's bytes out of a ZIP / TAR / TAR.GZ / GZIP archive without extracting. entry_path must match an entry exactly (not a glob). Returns content as UTF-8 text when valid (content field) or base64-encoded raw bytes when not (content_base64). Capped at max_bytes (default 1 MiB) with truncated=true when the entry exceeds the cap. Also surfaces detected content_type + per-format attributes so callers don't need a separate list_archive_contents to know what they're looking at. Useful for agent flows like 'pull pyproject.toml out of source.tar.gz to check the Python version' or 'read the .github/workflows/ci.yml from a release archive'. Errors with entry-not-found when entry_path doesn't match any archive entry.",
	}, instrument(h.metrics, "read_file_in_archive", h.readFileInArchiveHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "detect_project",
		Description: "Inspect a single directory and report which project type(s) it matches based on canonical indicator files (go.mod → go, package.json → node, Cargo.toml → rust, pyproject.toml/requirements.txt/Pipfile → python, Gemfile → ruby, pom.xml → java-maven, build.gradle → java-gradle, *.csproj / *.slnx / Directory.Build.props → dotnet, *.tf → terraform, docker-compose.yml → docker-compose; plus static-site generators: hugo.toml → hugo, _config.yml → jekyll, .eleventy.js / eleventy.config.* → eleventy, astro.config.* → astro, gatsby-config.* → gatsby, mkdocs.yml → mkdocs, docusaurus.config.* → docusaurus, pelicanconf.py → pelican). A directory can match multiple types simultaneously (a Go module that also ships docker-compose.yml hits both). Output includes the matched indicator filename for each type so callers can audit detection decisions. Non-recursive — only the given directory's own listing is read.",
	}, instrument(h.metrics, "detect_project", h.detectProjectHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_projects",
		Description: "Walk a root directory and return every project root found. A project root is a directory whose contents match a registered project-type indicator. By default the walker stops at the first match per branch (the 'find me all my Go repos' shape) — pass nested=true to also surface sub-projects inside matched roots (monorepo workspaces, vendored deps). Filter to specific types with 'types': ['go','rust',…]. Prune the walk with 'excludes' (basename globs like ['node_modules', '.git', 'target']) or respect_gitignore. Honours the same timeout / cancellation contract as the search tool — on expiry the partial result set is returned with cancelled=true, never an error.",
	}, instrument(h.metrics, "find_projects", h.findProjectsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_matches",
		Description: "Scan a directory tree for lines matching an RE2 regex, with optional context windows. Combines a CEL pre-prune (type predicates + typed attributes, same vocabulary as the search tool) with a line-level regex scan: 'expr' picks the candidate files cheaply (e.g. is_source && language == \"go\"), then 'pattern' runs against each line and reports every hit with its line number and the requested before/after context. Returns line-level matches sorted by (path, line). Plain-text types (markdown / text / html / csv / json / xml / source/*) are scanned directly; structured documents (office / epub / pdf / email) are extracted to text first and scanned too, so a phrase inside an .epub or .docx is found; truly binary families (image / audio / video / archive / compiled binary) are filtered out. Inputs: pattern (required, RE2), expr (optional CEL pre-prune), context_before / context_after (surrounding lines per hit), max_matches_per_file (cap; the scanner keeps reading past the cap until pending After windows are filled). Same dir / dirs / excludes / respect_gitignore / timeout_seconds / cancellation contract as search. Use when an agent needs 'find references to X' or 'show every TODO with context' — replaces the two-call search-then-read_lines dance with one call.",
	}, instrument(h.metrics, "find_matches", h.findMatchesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "imported_by",
		Description: "Cross-file reverse-dependency lookup: list every source file that imports a given module. Builds a project-wide import graph from the per-file import lists (the same data the search tool surfaces as 'imports'), then inverts it — answering 'who depends on X?', which the per-file search tool cannot. Inputs: module (required — the import string exactly as written in source, e.g. 'github.com/spf13/cobra', 'react', 'numpy'), mode ('exact' default / 'prefix' / 'regex'), plus the usual expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Accurate across every language whose imports are extracted (Go via AST; Python/Java/C#/PHP/Perl/R/MATLAB/Scala via the import-shape extractors). Output: importers[] {path, language} sorted by path + count + total_files. Honours the partial-result contract — a timeout returns the graph built so far with cancelled=true.",
	}, instrument(h.metrics, "imported_by", h.importedByHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_definition",
		Description: "Locate where a function or type is DEFINED across a tree — the symbol-aware complement to find_matches (which is text-level regex). Builds the project-wide definition index from the per-file functions / type_names lists and looks up an exact symbol name. Inputs: symbol (required, exact name — e.g. 'ServeHTTP', 'OrderService'), kind ('function' / 'type' / empty for both), plus expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. NOTE: only languages with symbol extraction populate definitions — Go (AST) + Python / Java / C# / PHP / Perl / R / MATLAB / Scala; symbols defined only in other languages won't appear (use find_matches there). Output: definitions[] {path, language, kind} sorted by path + count + total_files.",
	}, instrument(h.metrics, "find_definition", h.findDefinitionHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "references",
		Description: "Find every USAGE of a symbol with file + line — the canonical IDE 'find references', and the complement to find_definition. Unlike who_calls (which lists referencing files), this pinpoints the exact lines and tags each: 'call' (call site), 'type' (used as a field / parameter / return / variable / generic-arg type), or 'value' (Go function value passed as an argument, e.g. an AddTool / HandleFunc handler). Uses the code graph's reference index to pre-filter to the files that reference the symbol (cheap on a warm index), then parses only those to locate the lines — cost scales with usages, not tree size. Input: symbol (required, exact name — function OR type), kind ('call' / 'type' / 'value' / empty for all), plus expr (defaults to is_source) / dir / dirs / excludes / timeout_seconds. Output: references[] {path, line, kind, language} sorted by path then line, count, total_files. Coverage follows the reference graph: Go + the tree-sitter languages for calls and type usages, Go-only for value passing; JavaScript / Ruby / Perl / R / MATLAB capture call sites only. HEURISTIC and name-based (same caveats as who_calls): same-name symbols across packages/types collapse together.",
	}, instrument(h.metrics, "references", h.referencesHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "code_graph",
		Description: "Project-wide code-structure overview built from the cross-file import + definition graph. Returns: import_hubs (modules ranked by how many files import them — the most-depended-on dependencies), high_fan_out (files importing the most modules — the most coupled files), duplicate_definitions (function/type names defined in more than one file — name collisions, overloads, copy-paste), a per-language file breakdown, and totals (files / distinct modules / distinct symbols). Inputs: expr (defaults to is_source — narrow with language == \"go\" etc.), top (cap each ranked list, default 20), plus dir / dirs / excludes / respect_gitignore / timeout_seconds. Reconnaissance for 'what does this codebase depend on most?' and 'where's the coupling?' without retrieving individual matches. Pair imported_by / find_definition for drill-down. Partial-result contract honoured on timeout.",
	}, instrument(h.metrics, "code_graph", h.codeGraphHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "who_calls",
		Description: "Cross-file reverse usage lookup: list every source file that calls/references a given function or type name. The third leg of the code graph (complements imported_by for modules and find_definition for definitions). Input: symbol (required, exact name — e.g. 'ServeHTTP', 'process', 'Widget'), plus expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Reference extraction covers Go (go/ast) + the tree-sitter languages (Rust / TypeScript / JavaScript / Python / Java / C# / Ruby / Swift / Kotlin / Scala / PHP / Perl / R / MATLAB / C / C++); callers written in other languages won't appear. It also picks up TYPE usages (a type named as a field / parameter / return / variable / generic-arg type; issue #398) for Go and the statically-typed tree-sitter languages (Rust / TypeScript / Python / Java / C# / C / C++ / Kotlin / Swift / Scala / PHP), so querying a type name finds its users, not just constructor calls; JavaScript / Ruby / Perl / R / MATLAB have no static types and capture call sites only. For Go it also captures function/method VALUES passed as call arguments (issue #421), so a callback-registered handler (AddTool / HandleFunc) shows its registration site as a user. Name-based resolution: a call pkg.Foo() or x.Method() is keyed by 'Foo' / 'Method', so same-named symbols across types/packages collapse together (reliable for 'who uses X', heuristic for disambiguation). Output: callers[] {path, language} sorted by path + count + total_files.",
	}, instrument(h.metrics, "who_calls", h.whoCallsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "dead_code",
		Description: "List candidate unreferenced definitions — functions / types defined somewhere in the tree whose name never appears as a call/reference anywhere in it. Built from the cross-file reference graph. Input: expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Restricted to definitions in languages with reference extraction (Go + Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++) so languages we don't scan for calls aren't reported wholesale. Reflection-dispatched / runtime entry points that would always be false positives are excluded: Go package-init (`init`) + program entry (`main`), Go test-runner functions (TestXxx / BenchmarkXxx / FuzzXxx / ExampleXxx in *_test.go), and kong/cobra-style *Cmd command types. For Go, functions wired up by being passed as a value (the AddTool / HandleFunc callback pattern; issue #421) are correctly seen as referenced. The graph tracks TYPE usages as well as calls (issue #398) for Go and the statically-typed tree-sitter languages (Rust / TypeScript / Python / Java / C# / C / C++ / Kotlin / Swift / Scala / PHP) — a type used only as a field / parameter / return / variable type counts as referenced, eliminating that false-positive class; JavaScript / Ruby (no static types) still track call references only. IMPORTANT — the rest are still CANDIDATES, not authoritative: name-based heuristic with known false positives — exported/public API used only by external callers, entry points (main), dynamic dispatch, reflection, and same-name collisions all surface here. Use as a starting point for manual review, never as a delete list. Output: candidates[] {path, language, kind, symbol} sorted by path + count + total_files.",
	}, instrument(h.metrics, "dead_code", h.deadCodeHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "test_gaps",
		Description: "List production source files whose functions are never referenced from a test — candidate untested code. Built from the cross-file reference graph (same machinery as dead_code, filtered to 'not referenced from a *_test file'): a function counts as tested when its name appears as a reference in any file the detector flagged is_test_file. Input: expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Restricted to languages with reference extraction (Go + Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++). Output: gaps[] {path, language, function_count, untested_count, untested_functions[], fully_untested} sorted fully-untested-first then by untested_count desc — plus count, total_files. fully_untested=true means NOT ONE function in the file is referenced from a test (the clearest gaps). HEURISTIC — name-based and DIRECT-reference only: code exercised only transitively (test → A → B, B never named in a test) reads as untested, and same-name collisions can mislead. Candidates for review, not a coverage report — for precise line/branch coverage use a coverage profile (see the coverage_gaps roadmap). No test framework or instrumentation required.",
	}, instrument(h.metrics, "test_gaps", h.testGapsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "impact",
		Description: "Transitive reverse-dependency closure for a function — the blast radius of changing it. Where who_calls answers one hop ('who calls Y directly?'), impact returns EVERY function that reaches Y through the call graph, with the depth at which each was found (1 = direct caller). BFS over the per-function call graph; cycles handled. Input: symbol (required, exact function/method name), max_depth (0 = unbounded; 1 = direct callers only), plus the usual expr (defaults to is_source) / dir / dirs / excludes / timeout_seconds. Output: dependents[] {symbol, depth, paths[]} sorted by depth asc then name, plus count, max_depth_reached, total_files. Use before a refactor to gauge what a signature/behaviour change touches. HEURISTIC, name-based (same caveats as who_calls / calls): same-name collisions, interface / reflection dispatch, and table-driven indirection can over- or under-count. The import-level equivalent ('what transitively imports this file') is not yet available — it needs package resolution the graph doesn't carry.",
	}, instrument(h.metrics, "impact", h.impactHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "call_path",
		Description: "Show HOW one function reaches another — the shortest call path from `from` to `to` through the call graph. Where impact gives the whole closure of callers and who_calls/calls give one hop, call_path gives the actual route (e.g. 'how does the HTTP handler reach the DB write?'). BFS over the per-function call graph. Input: from (required, the calling function), to (required, the target), max_depth (0 = unbounded), plus the usual expr (defaults to is_source) / dir / dirs / excludes / timeout_seconds. Output: path[] {symbol, paths[]} ordered from→to (with the file(s) defining each step), reachable (bool), length (hops), total_files. Empty path + reachable=false when `to` isn't reachable from `from`. HEURISTIC, name-based (same caveats as impact / calls): same-name collisions and interface / reflection dispatch can produce a spurious or missing route.",
	}, instrument(h.metrics, "call_path", h.callPathHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "api_diff",
		Description: "Detect breaking changes to the EXPORTED public API surface between two trees — a release gate. Builds a code graph over each tree and diffs the set of exported function/type names: removed[] (present in tree_a, gone in tree_b → the breaking set) and added[] (new surface in tree_b). breaking=true iff anything was removed. 'Exported' = an upper-cased first rune (Go's export rule exactly; a heuristic for languages whose visibility isn't case-based — narrow with expr to a single language for accuracy). Input: tree_a (required, baseline/released), tree_b (required, candidate/proposed), expr (defaults to is_source), plus the usual workers / excludes / prune_build_artefacts / timeout_seconds. Output: removed[] / added[] each {symbol, kind: function|type} sorted by name, breaking (bool), removed_count, added_count, exported_a, exported_b. v1 compares NAME + KIND presence, not signatures — a changed function signature with the same name is NOT flagged; a kind change (func → type) shows as that name removed under one kind and added under the other.",
	}, instrument(h.metrics, "api_diff", h.apiDiffHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coverage_gaps",
		Description: "Report functions whose statement coverage is below a threshold, from a Go coverage profile — the precise complement to test_gaps (which needs no profile but only sees direct test references). Reads a profile produced by 'go test -coverprofile=cover.out ./...', resolves each profiled file to disk (its import path minus the module prefix from go.mod), splits it into functions, and sums each function's covered vs total statements. Input: profile (required, path to the profile), dir (module root holding go.mod; default '.'), threshold (coverage fraction 0..1 — report functions strictly below it; 0/omit = 1.0 = every function not fully covered; 0.8 = under 80%). Output: gaps[] {path, function, start_line, end_line, covered_statements, total_statements, covered_pct, fully_uncovered} sorted worst-coverage-first then biggest gap, plus files_analysed, profile_mode, count. Go coverage profiles only. Unlike test_gaps this catches partially-tested functions and counts transitive coverage correctly — but requires actually running the tests with -coverprofile first.",
	}, instrument(h.metrics, "coverage_gaps", h.coverageGapsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coupling",
		Description: "Afferent/efferent coupling + instability (Robert C. Martin's metrics) — the architecture-health report the import fan-out guard only gestures at. Counts distinct node→node import edges among first-party nodes. For each node: afferent (Ca = how many first-party nodes import it), efferent (Ce = how many it imports), instability (I = Ce/(Ca+Ce), 0=maximally stable/depended-upon, 1=maximally unstable/depends-on-others). Ranked most-depended-upon (high Ca) then most unstable (high I) — high Ca + high I marks a fragile hub: heavily relied upon yet itself volatile, the riskiest seam to change. The granularity and first-party boundary are selected by the build manifest at dir: GO (go.mod) → packages (node = module path + directory); RUST (Cargo.toml) → crates (node = crate, first-party = workspace member crates; crate::/self::/super:: are intra-crate); JVM — Java / Kotlin / Scala (Maven / Gradle / sbt) → packages and C# (.sln / .csproj) → namespaces (node = the declared package / namespace, first-party = the set the tree itself declares, so JDK / .NET BCL / third-party imports are ignored; a mixed Java+Kotlin project graphs as one package set); PYTHON (pyproject.toml / setup.py) → packages (node = the dotted directory path beneath the import root — a top-level src/ if present, else the root; first-party = the packages the tree occupies; both absolute imports and relative imports — from . import x, from ..pkg import y — are resolved against the importing file's package); JS/TS (package.json / tsconfig.json) → directory modules (node = a file's directory relative to the root; first-party = relative imports ./x ../y, resolved to the target directory; bare specifiers like react are external and tsconfig path aliases are not yet resolved); PHP (composer.json) → namespaces (node = the declared `namespace App\\Services`, backslash-separated; first-party = the namespaces the tree declares, so vendor/third-party namespaces are ignored); PERL (cpanfile / Makefile.PL / Build.PL / dist.ini) → packages (node = the declared `package Foo::Bar`, ::-separated; first-party = the packages the dist declares, so CPAN modules + pragmas like strict are ignored). Input: dir (the PROJECT ROOT holding go.mod, Cargo.toml, a JVM build file, a .sln / .csproj, a Python manifest, package.json / tsconfig.json, composer.json, or a Perl dist manifest; default '.'), expr (defaults to is_source), top (cap; 0=all), plus the usual excludes / prune_build_artefacts / timeout_seconds. Output: module (project identity), packages[] {package, afferent, efferent, instability}, count. Returns module=\"\" + empty packages when no recognised manifest is at dir. dirs[] uses the first root as the project root. More languages tracked in #467.",
	}, instrument(h.metrics, "coupling", h.couplingHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "circular",
		Description: "Detect circular dependencies — strongly-connected components (size > 1) in the first-party import graph, found via Tarjan's SCC algorithm. A cycle means a group of packages/crates/namespaces/directory-modules that (transitively) import each other, the seams that make a codebase hard to test, build incrementally, or reason about. The graph and first-party boundary are selected by the build manifest at dir, identical to the coupling tool — GO (go.mod) → packages; RUST (Cargo.toml) → crates; JVM Java/Kotlin/Scala (Maven / Gradle / sbt) → packages; C# (.sln / .csproj) → namespaces; PYTHON (pyproject.toml / setup.py) → packages; JS/TS (package.json / tsconfig.json) → directory modules; PHP (composer.json) → namespaces; PERL (cpanfile / Makefile.PL) → packages — so cycle detection is multi-language. Input: dir (the PROJECT ROOT holding a recognised manifest; default '.'), expr (defaults to is_source), plus excludes / prune_build_artefacts / timeout_seconds. Output: module (project identity), cycles[] {nodes (sorted), length} ranked largest-first, count. Returns module=\"\" + empty cycles when no recognised manifest is at dir. Self-edges are never reported.",
	}, instrument(h.metrics, "circular", h.circularHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "review",
		Description: "Diff-scoped review gate. Resolves the files changed in the git diff, runs the per-file analyses scoped to them, and returns the findings plus an overall pass/warn/fail verdict — the pre-commit / PR gate that composes the individual analysis tools. Checks: cyclomatic complexity (a changed-file function whose cyclomatic complexity exceeds max_complexity, default 15, is a FAIL finding), cognitive complexity (whose SonarSource nesting-weighted complexity exceeds max_cognitive, default 15, is a FAIL finding — catches deeply-nested code a modest cyclomatic count misses; only where computed, Go + most tree-sitter languages), and dead-code (a candidate unreferenced symbol in a changed file is a WARN finding; skip with skip_dead_code). The walk covers 'dir' (so the dead-code graph sees the whole project) and findings are filtered to the changed set. Input: dir (the git working directory + walk root; default '.'), base (empty = uncommitted changes vs HEAD for pre-commit; a ref like 'origin/main' = <base>...HEAD for a PR gate), max_complexity, max_cognitive, skip_dead_code, expr (defaults to is_source), plus the usual excludes / prune_build_artefacts / timeout_seconds. Output: base, changed_files[], files_analysed, findings[] {rule, level (warn|fail), message, path, symbol, start_line, end_line}, verdict (fail when any fail finding, warn when any warn and no fail, else pass), warn_count, fail_count. Verdict is pass when the diff is empty. Deleted files are excluded; untracked files are not in the diff. Errors when dir is not a git repository.",
	}, instrument(h.metrics, "review", h.reviewHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "unused_exports",
		Description: "List exported symbols (functions / types) that are referenced ONLY from within their own package — candidates to unexport and shrink the package's public API surface. The subtler complement to dead_code: dead_code finds symbols used nowhere; unused_exports finds symbols used somewhere, but never across a package boundary. Resolves each file to a package identity, then keeps exported symbols whose every referencing file lives in the defining package. Languages (9): Go (capitalised name + go.mod import path), Python (public/_private convention + dir), Rust (`pub` + module dir), TypeScript / JavaScript (`export` + the file as ES module), Java / C# (`public` keyword + dir — approximate for C#, whose namespace can decouple from the directory), and Kotlin / Scala (default-public minus private/internal/protected + dir). PHP (top-level symbols have no visibility keyword), Ruby / Swift, and Perl / R / MATLAB are out of scope. Input: dir (default '.'; for Go this is the MODULE ROOT holding go.mod), expr (defaults to is_source), plus the usual excludes / prune_build_artefacts / timeout_seconds. Output: module (the go.mod path, empty when none), candidates[] {symbol, kind: function|type, path, package} sorted by package then symbol, count. HEURISTIC — reflection / framework dispatch (kong …Cmd, Go test entries) is excluded, but external consumers outside the walked tree, interface satisfaction, and same-name collisions across packages can still mislead; a review list, not an auto-unexport list. Uses the #398 type-usage references so type exports aren't wrongly flagged.",
	}, instrument(h.metrics, "unused_exports", h.unusedExportsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "calls",
		Description: "Forward call lookup: list the distinct functions that a given function calls — \"what does Y call?\". The forward complement to who_calls. Input: symbol (required, exact function/method name), plus expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Built by attributing each call site to its enclosing function (span containment for tree-sitter; go/ast for Go). Coverage: Go + the tree-sitter languages whose grammar exposes function spans (Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++). Name-based and unioned across all functions sharing the name: a callee pkg.Foo() / x.Method() is keyed by 'Foo' / 'Method'; dynamic dispatch and function values aren't resolved; calls inside nested closures attribute to the enclosing named function. Output: callees[] (sorted distinct names) + count + total_files.",
	}, instrument(h.metrics, "calls", h.callsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "trace",
		Description: "Both directions of a symbol's call graph in one call: its callers (who_calls — files that call it) and its callees (calls — functions it invokes), optionally plus the transitive caller closure (impact). A convenience over running who_calls / calls / impact separately, sharing one code-graph build. Input: symbol (required, exact function/method name), impact_depth (0 = omit the closure; N > 0 = include transitive callers up to N hops), plus expr (defaults to is_source) / dir / dirs / excludes / respect_gitignore / timeout_seconds. Output: symbol, defined_on (owning types when the name is a method), callers[] {path, language}, callees[] (distinct names), impact[] {symbol, depth, paths} when requested, callers_count / callees_count, total_files. Name-based, same caveats as who_calls / calls.",
	}, instrument(h.metrics, "trace", h.traceHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "complexity",
		Description: "List functions ranked by cyclomatic complexity (worst-first) — find the gnarliest, hardest-to-maintain functions in a tree. Each row: {path, function, complexity, cognitive_complexity, start_line, end_line, lines}. Complexity is gocyclo-style (1 + branch points: if / for / while / case / && / ||). cognitive_complexity is the SonarSource nesting-weighted metric — it weights deeply-nested control flow more heavily than flat sequences, so it tracks how hard code is to UNDERSTAND (not just how many paths it has); it is present for Go (precise) plus every tree-sitter language except Swift, and omitted there (tracked in #491). Input: expr (defaults to is_source — narrow with language == \"go\"), top (cap, default 50), plus dir / dirs / excludes / respect_gitignore / timeout_seconds. Coverage: Go (stdlib AST) + every tree-sitter language (Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++ / Python / Java / C# / PHP / Perl / R / MATLAB / Scala); Perl / R under-count (sparser decision queries). Directionally accurate for RANKING hotspots — the exact number depends on per-grammar node coverage, so treat it as 'which functions are scariest', not a certified metric. For a file-level filter use the search tool's max_complexity attribute (e.g. 'is_source && max_complexity > 15'); this tool is the per-function drill-down.",
	}, instrument(h.metrics, "complexity", h.complexityHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "watch_search",
		Description: "Watch directories for a BOUNDED window and return every new / changed file that matches a CEL expression — the inverse of search ('tell me when X appears' instead of 'what already matches'). Same CEL vocabulary as the search tool (is_image, is_pdf, is_source && language == \"go\", size > 1000000, body.contains(\"error\"), …). Watches recursively (subdirectories created during the window are picked up). Blocks until duration_seconds elapses (default 30s, hard-capped at 600s), max_events matches are collected, or the call is cancelled — whichever comes first; then returns the collected matches. Inputs: expr (optional CEL filter), dir / dirs, duration_seconds (how long to watch), max_events (return early after N matches), plus the usual ocr_images / compute_hashes / with_phash / with_xattrs / include_body / excludes / respect_gitignore flags. Output: matches[] (same shape as search) + watched_seconds + hit_max_events. Use for short 'wait for the next screenshot / build artefact / download' flows; for open-ended streaming use the CLI 'watch' subcommand. Issue #211.",
	}, instrument(h.metrics, "watch_search", h.watchSearchHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_presets",
		Description: "List every named search recipe ('preset') available to the query_preset tool — each preset bakes a vetted CEL filter + sensible sort / limit defaults for a common workflow. Use this to discover what's available before calling query_preset. Filesystem presets: recent_changes (mod_time last 7 days), recent_photos (taken_at last 30 days), old_drafts (markdown drafts older than 90 days), large_files (> 100 MB), large_binaries (compiled binaries > 100 MB), suspicious_files (disguised files / btime anomalies; auto-enables check_disguised), failed_tests (test files with FAIL/FIXME/XXX in comments; auto-enables include_body), system_metadata (.DS_Store / Thumbs.db / Desktop.ini / .directory). Repo-aware presets (auto-enable with_git): recent_commits (commits last 7 days — repo sibling of recent_changes), hot_files (top-20 by git_commit_count across all tracked files — refactor candidates), hotspots (top-20 source files by complexity × churn — max_complexity × git_commit_count — the 'what to refactor first' ranking), prod_code (is_source && is_git_tracked && !is_test_file && !is_generated_code), untracked_code (source not in git, not gitignored — 'did I forget to commit?'), generated_code (codegen markers via is_generated_code), test_files (per-language test conventions). Output: presets[] with name + description; pass the name to query_preset to run.",
	}, instrument(h.metrics, "list_presets", h.listPresetsHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "query_preset",
		Description: "Run a named search recipe by name. Each preset bakes a vetted CEL filter + sensible defaults — recent_changes / recent_photos / old_drafts / large_files / large_binaries / suspicious_files / failed_tests / system_metadata. Per-call overrides: dir / dirs to scope the walk, limit to override the preset's default cap, excludes / respect_gitignore / follow_symlinks for the usual walk-pruning options. Returns the same Match shape as the search tool. Discover available presets via list_presets. Issue #168 sub-feature B.",
	}, instrument(h.metrics, "query_preset", h.queryPresetHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "resolve_project_for_path",
		Description: "Given an arbitrary file or directory path, walk up the directory chain (unbounded — terminates at the filesystem root) and return the nearest ancestor that matches a registered project type (go.mod → go, package.json → node, Cargo.toml → rust, pyproject.toml/requirements.txt/Pipfile → python, Gemfile → ruby, pom.xml → java-maven, build.gradle → java-gradle, *.csproj / *.slnx / Directory.Build.props → dotnet, *.tf → terraform, docker-compose.yml → docker-compose; plus static-site generators hugo / jekyll / eleventy / astro / gatsby / mkdocs / docusaurus / pelican). The MIDDLE question between detect_project (single-dir, what is THIS dir?) and find_projects (recursive, what's under this tree?): given a file path, what project owns it? Returns project_root (matched directory; empty when no ancestor matches), project_types (all matching types — a Go module that also ships docker-compose.yml hits both), and the indicators that fired. Use when an agent has a path from elsewhere and needs the project context — e.g. to scope a follow-up search or decide which language-specific tool to invoke.",
	}, instrument(h.metrics, "resolve_project_for_path", h.resolveProjectForPathHandler))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "monitor_info",
		Description: "Report this file-search-on server's monitoring-dashboard URL and the registry of sibling instances (other concurrently-running file-search-on processes that have a dashboard). Use to find the live dashboard to open, or to switch between multiple agents' dashboards. Pass enable=true to start this server's dashboard on an OS-assigned localhost port if it wasn't launched with --monitor / --monitor-addr (idempotent — returns the same URL on repeat calls). Output: enabled + url for this server, and peers[] (each with url / mode / pid / working_dir / version / started_at; the entry for this server is flagged is_self). Dashboards are localhost-only.",
	}, instrument(h.metrics, "monitor_info", h.monitorInfoHandler))

	return s
}

// Run starts an MCP server on stdio with the given index and default
// per-call timeout, and blocks until the transport closes or ctx is
// cancelled.
func Run(ctx context.Context, version string, idx index.Index, defaultTimeout time.Duration, embedDefaults EmbedDefaults, opts ...Option) error {
	return New(version, idx, defaultTimeout, embedDefaults, opts...).Run(ctx, &mcp.StdioTransport{})
}

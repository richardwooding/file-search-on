package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// watchSearchMaxDuration is the hard ceiling on how long a single
// watch_search call may block, regardless of the requested
// duration_seconds. MCP is request/response — an unbounded watch would
// hang the client's tool call forever. 600s (10 min) is generous for
// "wait for the next build artefact / screenshot / download" flows
// while still guaranteeing the call returns. The CLI `watch` subcommand
// is the unbounded streaming counterpart.
const watchSearchMaxDuration = 600 * time.Second

// watchSearchDefaultDuration applies when duration_seconds is omitted.
const watchSearchDefaultDuration = 30 * time.Second

// WatchSearchInput is the JSON-schema input for the `watch_search` tool.
type WatchSearchInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"CEL expression matched against each new / changed file — same vocabulary as the search tool (is_image, is_pdf, is_source && language == \"go\", size > 1000000, body.contains(\"error\"), …). Empty matches every new / changed file. Call list_attributes for the full schema."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to watch. Defaults to '.'. Ignored when 'dirs' is non-empty. Watched recursively — subdirectories created during the watch are picked up automatically."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to watch in one call. When non-empty, takes precedence over 'dir'."`
	DurationSeconds  float64  `json:"duration_seconds,omitempty" jsonschema:"How long to watch before returning the collected matches (seconds; fractions allowed). Defaults to 30s when omitted. Hard-capped at 600s (10 min) — this is a BOUNDED 'search over the near future', not an open-ended subscription. For unbounded streaming, use the CLI 'watch' subcommand instead."`
	MaxEvents        int      `json:"max_events,omitempty" jsonschema:"Return early once this many matching files have been collected, before duration_seconds elapses. 0 = no cap (watch the full duration). Use to bound the response when you only need 'the next file that matches'."`
	IncludeBody      bool     `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL expression as the 'body' string variable, so filters like body.contains(\"transformer\") run against newly-written files. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB)."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body string in bytes (default 1 MiB). Ignored when include_body is false."`
	OCRImages        bool     `json:"ocr_images,omitempty" jsonschema:"When true, run OCR over new image/* files via the registered provider (macOS Vision today). Populates 'body' + ocr_* so body.contains() queries fire on screenshots as they're saved. No-op on platforms without a registered provider. Issue #189."`
	OCRTimeoutMS     int      `json:"ocr_timeout_ms,omitempty" jsonschema:"Per-file OCR timeout in milliseconds. Default 10000 (10s) when zero."`
	ComputeHashes    bool     `json:"compute_hashes,omitempty" jsonschema:"When true, populate md5 / sha1 / sha256 (lowercase hex) on each match and expose them as CEL variables."`
	WithPHash        bool     `json:"with_phash,omitempty" jsonschema:"When true, compute the 64-bit perceptual hash of each new image and surface it as the 'phash' CEL string."`
	WithXattrs       bool     `json:"with_xattrs,omitempty" jsonschema:"When true, populate the xattr family of CEL variables (is_quarantined, quarantine_source_url, finder_tags, …). Darwin-only."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Basename glob patterns; matched directories are pruned from the watched tree. Same semantics as the search tool."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, honour a .gitignore at each watch root when registering directories."`
}

// WatchSearchOutput is the structured response. Matches carries every
// file that matched during the watch window (deduped per debounce). The
// watch ends when duration_seconds elapses, max_events is reached, or
// the call's parent ctx is cancelled — whichever comes first.
type WatchSearchOutput struct {
	CommonOutput
	Matches        []search.Match `json:"matches"`
	Count          int            `json:"count"`
	WatchedSeconds float64        `json:"watched_seconds"`
	// HitMaxEvents is true when the watch returned early because
	// max_events matches were collected before duration_seconds elapsed.
	HitMaxEvents bool `json:"hit_max_events,omitempty"`
}

func (h *handlers) watchSearchHandler(ctx context.Context, _ *mcp.CallToolRequest, in WatchSearchInput) (*mcp.CallToolResult, WatchSearchOutput, error) {
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, WatchSearchOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, WatchSearchOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, WatchSearchOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, WatchSearchOutput{}, err
	}
	// search.Watch registers opts.Roots only (not opts.Root), so fold
	// the single-dir case into the slice.
	roots := dirs
	if len(roots) == 0 {
		roots = []string{dir}
	}

	dur := watchSearchDefaultDuration
	if in.DurationSeconds > 0 {
		dur = time.Duration(in.DurationSeconds * float64(time.Second))
	}
	if dur > watchSearchMaxDuration {
		dur = watchSearchMaxDuration
	}

	wctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	var mu sync.Mutex
	var collected []search.Match
	hitMax := false
	onMatch := func(r search.Result) {
		mu.Lock()
		defer mu.Unlock()
		if in.MaxEvents > 0 && len(collected) >= in.MaxEvents {
			return // already full; ignore late debounce fires
		}
		collected = append(collected, search.MatchFrom(r))
		if in.MaxEvents > 0 && len(collected) >= in.MaxEvents {
			hitMax = true
			cancel() // got enough; stop watching so Watch returns now
		}
	}

	opts := search.Options{
		Roots:                  roots,
		Expr:                   in.Expr,
		IncludeAttributes:      true,
		Index:                  h.idx,
		IncludeBody:            in.IncludeBody,
		BodyMaxBytes:           in.BodyMaxBytes,
		OCRImages:              in.OCRImages,
		OCRTimeout:             time.Duration(in.OCRTimeoutMS) * time.Millisecond,
		ComputeHashes:          in.ComputeHashes,
		WithPHash:              in.WithPHash,
		ReadExtendedAttributes: in.WithXattrs,
		Excludes:               in.Excludes,
		RespectGitignore:       in.RespectGitignore,
	}

	start := time.Now()
	err = search.Watch(wctx, opts, content.DefaultRegistry(), onMatch)
	elapsed := time.Since(start).Seconds()

	// Watch returns nil on ctx cancel / deadline (the normal stop path).
	// Only a genuine setup error (e.g. invalid CEL, unreadable root)
	// surfaces as a tool error.
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, WatchSearchOutput{}, fmt.Errorf("watch_search: %w", err)
	}

	mu.Lock()
	matches := collected
	out := WatchSearchOutput{
		Matches:        matches,
		Count:          len(matches),
		WatchedSeconds: elapsed,
		HitMaxEvents:   hitMax,
	}
	mu.Unlock()
	if out.Matches == nil {
		out.Matches = []search.Match{}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

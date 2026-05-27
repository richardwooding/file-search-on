package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"text/template"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/monitor"
	"github.com/richardwooding/file-search-on/internal/search"
)

// WatchCmd is the continuous-query counterpart of `search`: instead of
// walking the tree once, it watches for new / modified files and emits
// each one that matches the CEL expression, until Ctrl-C. Issue #211.
type WatchCmd struct {
	Expr   string   `arg:"" optional:"" help:"CEL expression to match against new / changed files (e.g. 'is_image && body.contains(\"error\")'). Empty matches everything."`
	Dir    []string `short:"d" name:"dir" default:"." help:"Directory to watch. Repeatable; each is watched recursively (subdirectories created later are picked up automatically)."`
	Output string   `short:"o" name:"output" enum:"json,bare" default:"json" help:"Output per match: json (NDJSON, one object per line) | bare (path only)."`
	Format string   `name:"format" help:"Custom Go text/template applied per match (e.g. '{{.Path}}\\t{{.ContentType}}'). Overrides -o."`

	IndexPath        string        `name:"index-path" help:"Persistent attribute index (bbolt) — caches per-file parses + bodies (incl. OCR) across the watch and across runs."`
	Exclude          []string      `name:"exclude" help:"Basename glob to prune from the watched tree. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Honour a .gitignore at each watch root when registering directories."`
	Body             bool          `name:"body" help:"Make file body available as the 'body' CEL variable (needed for body.contains / has_secrets / etc.)."`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body string per file in bytes. 0 = 1 MiB default."`
	OCR              bool          `name:"ocr" help:"Run OCR over new image/* files (macOS Vision). Populates body + ocr_* for body.contains queries on screenshots."`
	OCRTimeout       time.Duration `name:"ocr-timeout" default:"10s" help:"Per-file OCR timeout."`
	WithHashes       bool          `name:"with-hashes" help:"Compute md5 / sha1 / sha256 for each matched file."`
	WithPHash        bool          `name:"with-phash" help:"Compute the perceptual hash (phash) of each new image."`
	WithXattrs       bool          `name:"with-xattrs" help:"Read macOS extended attributes for each new file (Darwin only)."`
	Monitor          bool          `name:"monitor" help:"Enable the read-only monitoring dashboard on an OS-assigned localhost port (no collision when many instances run concurrently). The URL is printed to stderr; sibling instances appear in each dashboard's peer switcher. Use --monitor-addr to pin a fixed port. (Watch mode has no MCP tools, so the dashboard can only be enabled at launch, not on demand.)"`
	MonitorAddr      string        `name:"monitor-addr" help:"Enable the monitoring dashboard on this fixed port (e.g. ':9090'). Binds 127.0.0.1 only. Overrides --monitor. Shows index cache stats + capabilities + a peer switcher at http://localhost:<port>/ (no MCP activity panel in watch mode)."`
}

func (c *WatchCmd) Run(ctx context.Context) error {
	var tmpl *template.Template
	if c.Format != "" {
		t, err := parseFormatTemplate(c.Format)
		if err != nil {
			return fmt.Errorf("parsing --format template: %w", err)
		}
		tmpl = t
	}

	idx, err := openIndex(c.IndexPath, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	if idx != nil {
		defer func() { _ = idx.Close() }()
	}

	opts := search.Options{
		Roots:                  c.Dir,
		Expr:                   c.Expr,
		IncludeAttributes:      true, // JSON / template output wants the full attrs
		Index:                  idx,
		IncludeBody:            c.Body,
		BodyMaxBytes:           c.BodyMaxBytes,
		OCRImages:              c.OCR,
		OCRTimeout:             c.OCRTimeout,
		WithPHash:              c.WithPHash,
		ComputeHashes:          c.WithHashes,
		ReadExtendedAttributes: c.WithXattrs,
		Excludes:               c.Exclude,
		RespectGitignore:       c.RespectGitignore,
	}

	// onMatch fires from debounce-timer goroutines, so serialise the
	// writes. Buffered stdout would risk losing a tail on Ctrl-C, so
	// write straight through.
	enc := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	onMatch := func(r search.Result) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case tmpl != nil:
			_ = tmpl.Execute(os.Stdout, search.MatchFrom(r))
			_, _ = fmt.Fprintln(os.Stdout)
		case c.Output == "bare":
			_, _ = fmt.Fprintln(os.Stdout, r.Path)
		default:
			_ = enc.Encode(search.MatchFrom(r)) // NDJSON
		}
	}

	// Optional monitoring dashboard. Watch mode has no MCP tool calls,
	// so no collector is attached — the dashboard shows cache stats +
	// capabilities only. Runs concurrently under the same ctx; the
	// deferred wait runs before idx.Close() (LIFO).
	monAddr := c.MonitorAddr // fixed port wins
	if monAddr == "" && c.Monitor {
		monAddr = ":0" // dynamic, OS-assigned
	}
	if monAddr != "" {
		mon := monitor.NewServer(monitor.Config{
			Version:   version,
			Mode:      "watch",
			Index:     idx,
			IndexPath: c.IndexPath,
		})
		monDone := make(chan struct{})
		go func() {
			defer close(monDone)
			if err := mon.Run(ctx, monAddr); err != nil {
				fmt.Fprintln(os.Stderr, "monitor:", err)
			}
		}()
		defer func() { <-monDone }()
	}

	fmt.Fprintf(os.Stderr, "watching %v for %q (Ctrl-C to stop)…\n", c.Dir, orTrue(c.Expr))
	return search.Watch(ctx, opts, contentpkg.DefaultRegistry(), onMatch)
}

func orTrue(expr string) string {
	if expr == "" {
		return "true"
	}
	return expr
}

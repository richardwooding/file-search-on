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

	IndexPath        string        `name:"index-path" help:"Persistent attribute index (bbolt) — caches per-file parses + bodies (incl. OCR) across the watch and across runs. Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/<basename>-<sha1[:6]>.db."`
	NoIndex          bool          `name:"no-index" help:"Disable the on-disk index entirely; use only in-memory caching for the watch. Useful when another file-search-on instance already holds the writer lock on the default index file."`
	Exclude          []string      `name:"exclude" help:"Basename glob to prune from the watched tree. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Honour a .gitignore at each watch root when registering directories."`
	Body             bool          `name:"body" help:"Make file body available as the 'body' CEL variable (needed for body.contains / has_secrets / etc.)."`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body string per file in bytes. 0 = 1 MiB default."`
	OCR              bool          `name:"ocr" help:"Run OCR over new image/* files (macOS Vision). Populates body + ocr_* for body.contains queries on screenshots."`
	OCRTimeout       time.Duration `name:"ocr-timeout" default:"10s" help:"Per-file OCR timeout."`
	WithHashes       bool          `name:"with-hashes" help:"Compute md5 / sha1 / sha256 for each matched file."`
	WithPHash        bool          `name:"with-phash" help:"Compute the perceptual hash (phash) of each new image."`
	WithXattrs       bool          `name:"with-xattrs" help:"Read macOS extended attributes for each new file (Darwin only)."`
	Monitor          bool          `name:"monitor" help:"No-op — the monitoring dashboard is on by default since v0.65.0. Kept for back-compat with pre-existing scripts. Use --no-monitor to opt out, --monitor-addr to pin a fixed port."`
	NoMonitor        bool          `name:"no-monitor" help:"Disable the read-only monitoring dashboard for this run. Useful for hermetic CI / sandboxed environments where binding a localhost port is undesirable. (Watch mode has no MCP tools, so the dashboard cannot be re-enabled mid-session.)"`
	MonitorAddr      string        `name:"monitor-addr" help:"Bind the monitoring dashboard on this fixed port (e.g. ':9090') instead of an OS-assigned dynamic port. Binds 127.0.0.1 only. Overrides the default dynamic-port behaviour. Shows index cache stats + capabilities + a peer switcher at http://localhost:<port>/ (no MCP activity panel in watch mode)."`
	Pprof            bool          `name:"pprof" help:"Mount Go runtime profiling endpoints (/debug/pprof/*) on the monitoring dashboard for live CPU / heap / goroutine profiling (go tool pprof http://localhost:<port>/debug/pprof/profile). Loopback-only — same 127.0.0.1 trust boundary as the dashboard. Off by default. Requires the dashboard, so it is a no-op with --no-monitor."`
	AllowOutsideHome bool          `name:"allow-outside-home" help:"Bypass the home-directory safety guard. By default watch refuses to start unless every -d directory is inside your home directory, to avoid a long-running watcher over system paths or an entire volume. Pass this to watch a directory elsewhere (other volumes, /opt, /srv) or in a container/CI runner where HOME isn't set."`
}

func (c *WatchCmd) Run(ctx context.Context) error {
	// Safety guard: refuse to watch a directory outside $HOME unless the
	// operator explicitly opts out. Runs before any side effects.
	if err := ensureUnderHome(c.Dir, c.AllowOutsideHome); err != nil {
		return err
	}

	var tmpl *template.Template
	if c.Format != "" {
		t, err := parseFormatTemplate(c.Format)
		if err != nil {
			return fmt.Errorf("parsing --format template: %w", err)
		}
		tmpl = t
	}

	idx, backend, err := openIndex(c.IndexPath, c.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

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
	if monAddr == "" && !c.NoMonitor {
		monAddr = ":0" // dynamic, OS-assigned (default since v0.65.0)
	}
	if c.Pprof && c.NoMonitor {
		fmt.Fprintln(os.Stderr, "warning: --pprof needs the monitoring dashboard; ignored because --no-monitor disables it")
	}
	if monAddr != "" {
		mon := monitor.NewServer(monitor.Config{
			Version:             version,
			Mode:                "watch",
			Index:               idx,
			IndexPath:           backend.Path,
			IndexBackend:        backend.Mode,
			EnablePprof:         c.Pprof,
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

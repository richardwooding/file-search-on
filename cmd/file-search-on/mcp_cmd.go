package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/mcpserver"
	"github.com/richardwooding/file-search-on/internal/monitor"
)

type MCPCmd struct {
	Transport         string        `name:"transport" enum:"stdio,http,sse" default:"stdio" help:"Transport: stdio (default; for desktop clients), http (Streamable HTTP, MCP 2025-03-26), or sse (DEPRECATED — HTTP+SSE, MCP 2024-11-05)."`
	Addr              string        `name:"addr" default:":8080" help:"host:port to bind for http or sse transports. Ignored for stdio."`
	Path              string        `name:"path" default:"/" help:"URL path prefix the handler is mounted at. Ignored for stdio."`
	IndexPath         string        `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/<basename>-<sha1[:6]>.db. The file is created on first use."`
	NoIndex           bool          `name:"no-index" help:"Disable the on-disk index entirely; use only the in-memory cache for the process lifetime. Useful when another file-search-on instance already holds the writer lock on the default index file, or for hermetic CI / one-shot runs."`
	BodyCacheMaxBytes int           `name:"body-cache-max-bytes" default:"268435456" help:"Total size cap (bytes) for the body cache inside the bbolt index file. Default 256 MiB. FIFO eviction by access time once exceeded. Only relevant when --index-path is set; in-memory indexes have no cap."`
	NoBodyCache       bool          `name:"no-body-cache" help:"Disable the body cache. LookupBody always misses; PutBody is a no-op. Bodies are re-extracted on every include_body query."`
	EmbeddingServer   string        `name:"embedding-server" env:"OLLAMA_HOST" default:"http://localhost:11434" help:"Default Ollama base URL for the search_semantic tool. Resolution order: --embedding-server flag > $OLLAMA_HOST env var > http://localhost:11434. Per-call 'embedding_server' input still overrides. Lazy connect — server starts without Ollama running; search_semantic fails clearly on first call if Ollama is unreachable."`
	EmbeddingModel    string        `name:"embedding-model" help:"Default Ollama embedding model for the search_semantic tool (e.g. nomic-embed-text, mxbai-embed-large). No default — pick a model you've pulled via 'ollama pull <name>'. Per-call 'model' input overrides. If neither is set, search_semantic returns 'no embedding model configured'."`
	Timeout           time.Duration `name:"timeout" default:"60s" help:"Default per-tool-call timeout (Go duration: 30s, 2m, 5m). Each search/read_attributes invocation is wrapped with this deadline. Per-call 'timeout_seconds' input on the search tool overrides this. Set to 0 to disable the default (not recommended — long-running calls can exceed MCP client read deadlines)."`
	Monitor           bool          `name:"monitor" help:"No-op — the monitoring dashboard is on by default since v0.65.0. Kept for back-compat with pre-existing scripts. Use --no-monitor to opt out, --monitor-addr to pin a fixed port."`
	NoMonitor         bool          `name:"no-monitor" help:"Disable the read-only monitoring dashboard for this run. Useful for hermetic CI / sandboxed environments where binding a localhost port is undesirable. monitor_info{enable:true} can still lazy-start the dashboard mid-session."`
	MonitorAddr       string        `name:"monitor-addr" help:"Bind the monitoring dashboard on this fixed port (e.g. ':9090') instead of an OS-assigned dynamic port. Binds 127.0.0.1 only. Overrides the default dynamic-port behaviour. Shows index cache stats, live tool-call activity, capabilities, and a peer switcher at http://localhost:<port>/."`
	Warm              bool          `name:"warm" help:"At startup, walk the warm root in the background to pre-populate the on-disk attribute cache so the first MCP tool call lands on a hot index. Off by default — opt in when the cwd is a project you actually want indexed (starting from $HOME would scan the entire home directory). Heavy attributes (hashes, OCR, body, snippet, phash, xattrs) stay off; only the cheap detector + per-type Attributes() parse runs. Runs concurrently with the MCP server, so clients connect immediately."`
	WarmDir           string        `name:"warm-dir" help:"Directory to warm. Defaults to the cwd at server start when --warm is set. Implies --warm when non-empty."`
	WarmWorkers       int           `name:"warm-workers" help:"Worker count for the warmer. Defaults to max(1, NumCPU/4) — a quarter of the cores so the MCP server, the agent driving it, and the rest of the box keep their headroom. Pass 1 for minimum CPU; ignored when --warm is off."`
	WarmTimeout       time.Duration `name:"warm-timeout" default:"10m" help:"Hard deadline on the warmer (Go duration: 30s, 5m). The MCP server keeps running if the warmer is killed by the deadline. Ignored when --warm is off."`
	WarmEmbeddings    bool          `name:"warm-embeddings" help:"At startup, walk the warm root in the background and pre-populate the search_semantic embeddings cache. Requires --embedding-model to be set. Reads every walked file's body and calls Ollama once per file (expensive: ~1s/file on localhost) — appropriate as a pre-flight to interactive use. Walks concurrently with the MCP server, so clients connect immediately. Combine with --warm to populate both caches in one walk."`
	Sandbox           bool          `name:"sandbox" help:"Restrict agent filesystem access to the cwd at server startup. Path-accepting MCP tool inputs (dir / dirs / path / tree_a / tree_b / hash_allowlist_path / hash_denylist_path) that resolve outside the sandbox are rejected with a clear error. Off by default; opt in when running file-search-on as an MCP server for an agent you want to scope to a single project. Combine with --sandbox-dir to specify explicit roots."`
	SandboxDir        []string      `name:"sandbox-dir" help:"Sandbox root directory. Repeatable for multiple roots (e.g. --sandbox-dir ~/Code/foo --sandbox-dir ~/Code/bar). Implies --sandbox. Symlinks pointing outside the sandbox are rejected; follow_symlinks=true is rejected entirely when the sandbox is active (the walker doesn't yet enforce sandbox per-entry)."`
}

func (m *MCPCmd) Run(ctx context.Context) error {
	idx, backend, err := openIndex(m.IndexPath, m.NoIndex, index.BodyCacheCap{MaxBytes: int64(m.BodyCacheMaxBytes), Disable: m.NoBodyCache})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	embedDefaults := mcpserver.EmbedDefaults{
		Server: m.EmbeddingServer,
		Model:  m.EmbeddingModel,
	}

	// Derive a cancellable child of the signal context so the dashboard
	// shuts down (and deregisters) when this command returns for ANY
	// reason — including stdio-transport EOF (client disconnect), which
	// does not cancel the parent signal context. The cancel defer is
	// registered AFTER controller.Wait() below so, by LIFO, the order on
	// return is: cancel() → Wait() (drains the dashboard) → idx.Close().
	ctx, cancelMonitor := context.WithCancel(ctx)

	// The monitoring dashboard is always *wireable* in MCP mode: we
	// build a collector (so tool-call history exists if the dashboard is
	// started later) and a controller (which owns the dashboard's lazy
	// lifecycle). The controller binds nothing until started — either
	// eagerly here when --monitor / --monitor-addr is set, or on demand
	// via the monitor_info MCP tool.
	collector := monitor.NewCollector()
	bodyCap := int64(0)
	if backend.Mode == BackendPersistent && !m.NoBodyCache {
		bodyCap = int64(m.BodyCacheMaxBytes)
	}
	monAddr := m.MonitorAddr // fixed port wins
	if monAddr == "" && !m.NoMonitor {
		monAddr = ":0" // dynamic, OS-assigned (default since v0.65.0)
	}
	// cwd at server start — dashboard warm endpoints walk this when the
	// operator doesn't supply ?dir=… on the POST. os.Getwd may fail in
	// pathological environments; an empty string means the buttons go
	// to 412 "no Cwd configured", which is the right failure mode.
	monCwd, _ := os.Getwd()
	warmAttrsFn := func(ctx context.Context, root string) error {
		return warmIndex(ctx, idx, root, m.WarmWorkers, os.Stderr)
	}
	warmBodyFn := func(ctx context.Context, root string) error {
		return warmBody(ctx, idx, root, m.WarmWorkers, os.Stderr)
	}
	var warmEmbedFn func(ctx context.Context, root string) error
	if m.EmbeddingModel != "" {
		embedder := embed.NewOllama(m.EmbeddingServer, m.EmbeddingModel)
		warmEmbedFn = func(ctx context.Context, root string) error {
			return warmEmbeddings(ctx, idx, root, m.WarmWorkers, embedder, os.Stderr)
		}
	}
	controller := monitor.NewController(ctx, monitor.Config{
		Version:             version,
		Mode:                "mcp-" + m.Transport,
		Index:               idx,
		Collector:           collector,
		EmbedServer:         m.EmbeddingServer,
		EmbedModel:          m.EmbeddingModel,
		IndexPath:           backend.Path,
		IndexBackend:        backend.Mode,
		IndexFallbackReason: backend.Reason,
		BodyCacheCap:        bodyCap,
		Cwd:                 monCwd,
		WarmAttrsFn:         warmAttrsFn,
		WarmBodyFn:          warmBodyFn,
		WarmEmbeddingsFn:    warmEmbedFn,
	}, addrOrDynamic(monAddr))
	// Drain the dashboard (and deregister from the peer registry) before
	// the index closes. Registered before cancelMonitor so, by LIFO,
	// cancelMonitor() runs first (triggering shutdown), then Wait()
	// blocks until the dashboard goroutine exits, then idx.Close() runs.
	defer controller.Wait()
	defer cancelMonitor()

	if monAddr != "" {
		if _, err := controller.EnsureStarted(); err != nil {
			fmt.Fprintln(os.Stderr, "monitor:", err)
		}
	}

	if m.Warm || m.WarmDir != "" {
		root := m.WarmDir
		if root == "" {
			if cwd, err := os.Getwd(); err == nil {
				root = cwd
			}
		}
		if root != "" {
			deadline := m.WarmTimeout
			if deadline <= 0 {
				deadline = 10 * time.Minute
			}
			warmCtx, cancelWarm := context.WithTimeout(ctx, deadline)
			go func() {
				defer cancelWarm()
				_ = warmIndex(warmCtx, idx, root, m.WarmWorkers, os.Stderr)
			}()
		} else {
			fmt.Fprintln(os.Stderr, "warm: could not resolve directory to warm; skipping")
		}
	}

	if m.WarmEmbeddings {
		if m.EmbeddingModel == "" {
			fmt.Fprintln(os.Stderr, "warm-embeddings: --embedding-model not set; skipping")
		} else {
			root := m.WarmDir
			if root == "" {
				if cwd, err := os.Getwd(); err == nil {
					root = cwd
				}
			}
			if root != "" {
				deadline := m.WarmTimeout
				if deadline <= 0 {
					deadline = 30 * time.Minute // embeddings are 10–100× slower than attrs
				}
				embedder := embed.NewOllama(m.EmbeddingServer, m.EmbeddingModel)
				warmCtx, cancelWarm := context.WithTimeout(ctx, deadline)
				go func() {
					defer cancelWarm()
					_ = warmEmbeddings(warmCtx, idx, root, m.WarmWorkers, embedder, os.Stderr)
				}()
			} else {
				fmt.Fprintln(os.Stderr, "warm-embeddings: could not resolve directory to warm; skipping")
			}
		}
	}

	mcpOpts := []mcpserver.Option{
		mcpserver.WithCollector(collector),
		mcpserver.WithMonitor(controller),
	}

	sandboxRoots := append([]string(nil), m.SandboxDir...)
	if m.Sandbox && len(sandboxRoots) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			sandboxRoots = []string{cwd}
		}
	}
	if len(sandboxRoots) > 0 {
		mcpOpts = append(mcpOpts, mcpserver.WithSandbox(sandboxRoots))
		fmt.Fprintf(os.Stderr, "sandbox: restricting agent access to %v\n", sandboxRoots)
	}

	switch m.Transport {
	case "http":
		return mcpserver.RunHTTP(ctx, version, m.Addr, m.Path, idx, m.Timeout, embedDefaults, mcpOpts...)
	case "sse":
		fmt.Fprintln(os.Stderr, "warning: --transport sse is DEPRECATED (MCP 2024-11-05); prefer --transport http for new clients.")
		return mcpserver.RunSSE(ctx, version, m.Addr, m.Path, idx, m.Timeout, embedDefaults, mcpOpts...)
	default:
		return mcpserver.Run(ctx, version, idx, m.Timeout, embedDefaults, mcpOpts...)
	}
}

// addrOrDynamic maps an empty monitor address to ":0" so the controller
// always has a bind target if started on demand (monitor_info{enable:true}).
func addrOrDynamic(addr string) string {
	if addr == "" {
		return ":0"
	}
	return addr
}

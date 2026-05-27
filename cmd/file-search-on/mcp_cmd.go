package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/mcpserver"
	"github.com/richardwooding/file-search-on/internal/monitor"
)

type MCPCmd struct {
	Transport         string        `name:"transport" enum:"stdio,http,sse" default:"stdio" help:"Transport: stdio (default; for desktop clients), http (Streamable HTTP, MCP 2025-03-26), or sse (DEPRECATED — HTTP+SSE, MCP 2024-11-05)."`
	Addr              string        `name:"addr" default:":8080" help:"host:port to bind for http or sse transports. Ignored for stdio."`
	Path              string        `name:"path" default:"/" help:"URL path prefix the handler is mounted at. Ignored for stdio."`
	IndexPath         string        `name:"index-path" help:"Persistent attribute index file (bbolt). When unset the server uses an in-memory cache that lives for the process lifetime; setting this makes the cache survive restarts. The file is created on first use."`
	BodyCacheMaxBytes int           `name:"body-cache-max-bytes" default:"268435456" help:"Total size cap (bytes) for the body cache inside the bbolt index file. Default 256 MiB. FIFO eviction by access time once exceeded. Only relevant when --index-path is set; in-memory indexes have no cap."`
	NoBodyCache       bool          `name:"no-body-cache" help:"Disable the body cache. LookupBody always misses; PutBody is a no-op. Bodies are re-extracted on every include_body query."`
	EmbeddingServer   string        `name:"embedding-server" default:"http://localhost:11434" help:"Default Ollama base URL for the search_semantic tool. Per-call 'embedding_server' input overrides. Lazy connect — server starts without Ollama running; search_semantic fails clearly on first call if Ollama is unreachable."`
	EmbeddingModel    string        `name:"embedding-model" help:"Default Ollama embedding model for the search_semantic tool (e.g. nomic-embed-text, mxbai-embed-large). No default — pick a model you've pulled via 'ollama pull <name>'. Per-call 'model' input overrides. If neither is set, search_semantic returns 'no embedding model configured'."`
	Timeout           time.Duration `name:"timeout" default:"60s" help:"Default per-tool-call timeout (Go duration: 30s, 2m, 5m). Each search/read_attributes invocation is wrapped with this deadline. Per-call 'timeout_seconds' input on the search tool overrides this. Set to 0 to disable the default (not recommended — long-running calls can exceed MCP client read deadlines)."`
	MonitorAddr       string        `name:"monitor-addr" help:"Enable the read-only monitoring dashboard on this port (e.g. ':9090'). Binds 127.0.0.1 only — the host part is ignored. Off when empty. Shows index cache stats, live tool-call activity, and registered capabilities at http://localhost:<port>/."`
}

func (m *MCPCmd) Run(ctx context.Context) error {
	idx, err := openIndex(m.IndexPath, index.BodyCacheCap{MaxBytes: int64(m.BodyCacheMaxBytes), Disable: m.NoBodyCache})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	embedDefaults := mcpserver.EmbedDefaults{
		Server: m.EmbeddingServer,
		Model:  m.EmbeddingModel,
	}

	// Optional monitoring dashboard. When enabled, attach a collector so
	// tool calls are recorded, and run the dashboard concurrently under
	// the same ctx. The deferred wait runs BEFORE idx.Close() (LIFO) so
	// the dashboard drains before the index is released.
	var mcpOpts []mcpserver.Option
	if m.MonitorAddr != "" {
		collector := monitor.NewCollector()
		mcpOpts = append(mcpOpts, mcpserver.WithCollector(collector))

		bodyCap := int64(0)
		if m.IndexPath != "" && !m.NoBodyCache {
			bodyCap = int64(m.BodyCacheMaxBytes)
		}
		mon := monitor.NewServer(monitor.Config{
			Version:      version,
			Mode:         "mcp-" + m.Transport,
			Index:        idx,
			Collector:    collector,
			EmbedServer:  m.EmbeddingServer,
			EmbedModel:   m.EmbeddingModel,
			IndexPath:    m.IndexPath,
			BodyCacheCap: bodyCap,
		})
		monDone := make(chan struct{})
		go func() {
			defer close(monDone)
			if err := mon.Run(ctx, m.MonitorAddr); err != nil {
				fmt.Fprintln(os.Stderr, "monitor:", err)
			}
		}()
		defer func() { <-monDone }()
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

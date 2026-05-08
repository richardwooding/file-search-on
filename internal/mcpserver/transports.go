package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// RunHTTP serves the MCP server over the modern Streamable HTTP transport
// (MCP spec 2025-03-26 onward). It blocks until ctx is cancelled or the
// listener returns an error other than http.ErrServerClosed. Each incoming
// request reuses the same *mcp.Server (and so the same idx + timeout).
//
// addr is a host:port pair (e.g. ":8080"). path is the URL prefix the handler
// is mounted at (e.g. "/" or "/mcp").
func RunHTTP(ctx context.Context, version, addr, path string, idx index.Index, defaultTimeout time.Duration) error {
	server := New(version, idx, defaultTimeout)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	return serveHTTP(ctx, addr, path, handler)
}

// RunSSE serves the MCP server over the deprecated HTTP+SSE transport
// (MCP spec 2024-11-05). Kept for backward compatibility with older clients;
// new deployments should prefer RunHTTP.
func RunSSE(ctx context.Context, version, addr, path string, idx index.Index, defaultTimeout time.Duration) error {
	server := New(version, idx, defaultTimeout)
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	return serveHTTP(ctx, addr, path, handler)
}

// serveHTTP wraps an http.Handler in an http.Server with graceful Shutdown
// triggered on ctx cancellation.
func serveHTTP(ctx context.Context, addr, path string, h http.Handler) error {
	if path == "" {
		path = "/"
	}
	mux := http.NewServeMux()
	mux.Handle(path, http.StripPrefix(stripFor(path), h))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// stripFor returns the prefix that should be stripped from incoming requests.
// "/" stays as-is (StripPrefix("") is a no-op); other paths get the trailing
// slash trimmed so the handler sees the rooted path it expects.
func stripFor(path string) string {
	if path == "/" || path == "" {
		return ""
	}
	if path[len(path)-1] == '/' {
		return path[:len(path)-1]
	}
	return path
}

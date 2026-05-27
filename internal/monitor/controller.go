package monitor

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// Controller owns the lazy lifecycle of a monitoring dashboard so it can
// be started eagerly at launch (--monitor / --monitor-addr) OR on demand
// mid-session via the monitor_info MCP tool. EnsureStarted is idempotent
// and concurrency-safe; once started, the dashboard runs under the
// serveCtx captured at construction (the command's signal context), so
// it outlives the per-tool-call context that may have triggered it.
type Controller struct {
	serveCtx context.Context
	cfg      Config
	addr     string

	mu      sync.Mutex
	started bool
	url     string
	done    chan struct{} // closed when the serve goroutine exits; nil until started
}

// NewController returns a controller that serves cfg's dashboard on addr
// (":0" for an OS-assigned port) when first started. serveCtx MUST be the
// long-lived command context, never a request context — a lazily-started
// dashboard keeps serving after the tool call that started it returns.
func NewController(serveCtx context.Context, cfg Config, addr string) *Controller {
	return &Controller{serveCtx: serveCtx, cfg: cfg, addr: addr}
}

// EnsureStarted binds + serves the dashboard on first success and returns
// its URL. Subsequent calls return the same URL without rebinding. A
// failed bind is NOT cached, so a later call can retry (e.g. after a
// port frees up).
func (c *Controller) EnsureStarted() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return c.url, nil
	}
	srv := NewServer(c.cfg)
	url, err := srv.Listen(c.addr)
	if err != nil {
		return "", err
	}
	c.started = true
	c.url = url
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		if serr := srv.Serve(c.serveCtx); serr != nil {
			fmt.Fprintln(os.Stderr, "monitor:", serr)
		}
	}()
	<-srv.Ready() // block until registered so a follow-up Peers() sees us
	return url, nil
}

// Info reports the dashboard URL and whether it is currently running.
func (c *Controller) Info() (url string, running bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.url, c.started
}

// Wait blocks until the dashboard's serve goroutine has fully exited
// (after serveCtx is cancelled and graceful shutdown + deregistration
// complete). Returns immediately if the dashboard was never started.
// The CLI defers this before closing the index so the registry entry is
// removed cleanly on shutdown.
func (c *Controller) Wait() {
	c.mu.Lock()
	d := c.done
	c.mu.Unlock()
	if d != nil {
		<-d
	}
}

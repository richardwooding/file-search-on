package monitor

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/content/ocr"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/projecttype"
)

//go:embed static/*
var staticFiles embed.FS

// Config wires the dashboard to the running process's state. Index is
// required (for cache stats); Collector is nil in watch mode (no MCP
// tool calls to report). The Embed* / IndexPath / BodyCacheCap fields
// are informational, surfaced on the overview panel.
type Config struct {
	Version      string
	Mode         string // "mcp-stdio" | "mcp-http" | "mcp-sse" | "watch"
	Index        index.Index
	Collector    *Collector
	EmbedServer  string
	EmbedModel   string
	IndexPath    string // "" → in-memory
	BodyCacheCap int64  // 0 → in-memory / no cap
}

// Server is the read-only monitoring HTTP server. Bind with Listen
// (which assigns the URL) then run with Serve; Run is the eager
// convenience wrapper that does both.
type Server struct {
	cfg       Config
	startedAt time.Time
	srv       *http.Server
	ln        net.Listener
	url       string
	ready     chan struct{} // closed by Serve once registered; see Ready
}

// NewServer builds a monitoring server from cfg.
func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg, startedAt: time.Now()}
}

// routes builds the dashboard mux. Returns an error only if the embedded
// static assets are missing (compile-time guaranteed, but surfaced
// rather than panicked).
func (s *Server) routes() (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/cache", s.handleCache)
	mux.HandleFunc("GET /api/activity", s.handleActivity)
	mux.HandleFunc("GET /api/capabilities", s.handleCapabilities)
	mux.HandleFunc("GET /api/peers", s.handlePeers)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("monitor static assets: %w", err)
	}
	mux.Handle("GET /", http.FileServerFS(sub))
	return mux, nil
}

// Listen binds a localhost-only TCP listener and returns the dashboard
// URL. addr's host is forced to 127.0.0.1 — only the port is honoured —
// so the dashboard (which can surface searched file paths) never binds a
// routable interface. Pass ":0" for an OS-assigned port (used by the
// dynamic --monitor mode so concurrent instances don't collide). Call
// Serve next to start serving.
func (s *Server) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", forceLoopback(addr))
	if err != nil {
		return "", fmt.Errorf("monitor listen: %w", err)
	}
	handler, err := s.routes()
	if err != nil {
		_ = ln.Close()
		return "", err
	}
	s.ln = ln
	s.url = fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
	s.ready = make(chan struct{})
	s.srv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.url, nil
}

// URL returns the bound dashboard URL, or "" before Listen.
func (s *Server) URL() string { return s.url }

// Ready returns a channel closed once Serve has registered this instance
// in the peer registry and is about to accept connections. Callers that
// launch Serve in a goroutine (the lazy Controller) wait on it so a
// follow-up Peers() read reflects this instance.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Serve serves the dashboard on the bound listener until ctx is
// cancelled, registering the instance in the peer registry for the
// duration so concurrent file-search-on processes can discover it. Listen
// must have been called first.
func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		return errors.New("monitor: Serve called before Listen")
	}

	deregister, regErr := Register(Entry{
		PID:        os.Getpid(),
		URL:        s.url,
		Mode:       s.cfg.Mode,
		Version:    s.cfg.Version,
		StartedAt:  s.startedAt,
		WorkingDir: workingDir(),
		IndexPath:  s.cfg.IndexPath,
	})
	if regErr != nil {
		fmt.Fprintln(os.Stderr, "monitor: peer registry unavailable:", regErr)
	}
	defer deregister()
	close(s.ready) // registered; Peers() now reflects this instance

	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Serve(s.ln) }()

	fmt.Fprintf(os.Stderr, "monitor dashboard: %s\n", s.url)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("monitor shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Run binds and serves in one call — the eager path used when the
// dashboard is enabled at launch.
func (s *Server) Run(ctx context.Context, addr string) error {
	if _, err := s.Listen(addr); err != nil {
		return err
	}
	return s.Serve(ctx)
}

// workingDir returns the process working directory, or "" on error.
// Surfaced in the peer switcher so an operator can tell instances apart.
func workingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// handlePeers returns every live registered dashboard instance, flagging
// the one that matches this server so the UI can mark "you".
func (s *Server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	peers := Peers()
	for i := range peers {
		if peers[i].URL == s.url {
			peers[i].IsSelf = true
		}
	}
	writeJSON(w, map[string]any{"self_url": s.url, "peers": peers})
}

// forceLoopback returns a 127.0.0.1 address preserving only the port of
// addr. A non-loopback host in addr is ignored (with a one-line warn) so
// the dashboard never escapes the local machine.
func forceLoopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No port separator (e.g. "9090"); treat the whole thing as a port.
		port = strings.TrimPrefix(addr, ":")
		host = ""
	}
	if host != "" && host != "127.0.0.1" && host != "localhost" {
		fmt.Fprintf(os.Stderr, "monitor: ignoring non-loopback host %q; binding 127.0.0.1 only\n", host)
	}
	if port == "" {
		port = "9090"
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- /api/overview ---

func (s *Server) handleOverview(w http.ResponseWriter, _ *http.Request) {
	indexBacking := "in-memory"
	if s.cfg.IndexPath != "" {
		indexBacking = s.cfg.IndexPath
	}
	writeJSON(w, map[string]any{
		"version":        s.cfg.Version,
		"mode":           s.cfg.Mode,
		"uptime_seconds": time.Since(s.startedAt).Seconds(),
		"pid":            os.Getpid(),
		"go_version":     runtime.Version(),
		"gomaxprocs":     runtime.GOMAXPROCS(0),
		"num_cpu":        runtime.NumCPU(),
		"index_backing":  indexBacking,
		"body_cache_cap": s.cfg.BodyCacheCap,
		"goroutines":     runtime.NumGoroutine(),
	})
}

// --- /api/cache ---

func (s *Server) handleCache(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Index == nil {
		writeJSON(w, map[string]any{"available": false})
		return
	}
	st := s.cfg.Index.Stats()
	writeJSON(w, map[string]any{
		"available": true,
		"attr": map[string]any{
			"hits": st.Hits, "misses": st.Misses, "puts": st.Puts,
			"stales": st.Stales, "errors": st.Errors,
			"hit_rate": rate(st.Hits, st.Misses),
		},
		"body": map[string]any{
			"hits": st.BodyHits, "misses": st.BodyMisses, "puts": st.BodyPuts,
			"stales": st.BodyStales, "evictions": st.BodyEvictions,
			"oversize": st.BodyOversize, "errors": st.BodyErrors,
			"hit_rate": rate(st.BodyHits, st.BodyMisses),
			"cap":      s.cfg.BodyCacheCap,
		},
		"embed": map[string]any{
			"hits": st.EmbedHits, "misses": st.EmbedMisses, "puts": st.EmbedPuts,
			"errors": st.EmbedErrors, "model_mismatches": st.EmbedModelMismatches,
			"hit_rate": rate(st.EmbedHits, st.EmbedMisses),
		},
	})
}

// rate returns the hit ratio (0..1) for a hits/misses pair, or 0 when
// there's been no traffic.
func rate(hits, misses uint64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// --- /api/activity ---

func (s *Server) handleActivity(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Collector == nil {
		writeJSON(w, map[string]any{
			"available": false,
			"reason":    "no MCP activity in this mode",
		})
		return
	}
	snap := s.cfg.Collector.Snapshot()
	writeJSON(w, map[string]any{
		"available": true,
		"snapshot":  snap,
	})
}

// --- /api/capabilities ---

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	// Content types grouped by family (prefix before the first "/").
	families := map[string][]string{}
	total := 0
	for _, ct := range content.DefaultRegistry().Types() {
		name := ct.Name()
		total++
		fam := name
		if before, _, ok := strings.Cut(name, "/"); ok {
			fam = before
		}
		families[fam] = append(families[fam], name)
	}
	famList := make([]map[string]any, 0, len(families))
	for fam, names := range families {
		sort.Strings(names)
		famList = append(famList, map[string]any{"family": fam, "count": len(names), "types": names})
	}
	sort.Slice(famList, func(i, j int) bool { return famList[i]["family"].(string) < famList[j]["family"].(string) })

	// Project types.
	projects := make([]map[string]string, 0)
	for _, pt := range projecttype.DefaultRegistry().Types() {
		projects = append(projects, map[string]string{"name": pt.Name, "description": pt.Description})
	}

	// OCR providers.
	var ocrProvider string
	if p := ocr.Default(); p != nil {
		ocrProvider = p.Name()
	}

	writeJSON(w, map[string]any{
		"content_types": map[string]any{"total": total, "families": famList},
		"project_types": projects,
		"ocr": map[string]any{
			"available":       ocr.HasProvider(),
			"active_provider": ocrProvider,
			"registered":      ocr.ListProviders(),
		},
		"embedder": map[string]any{
			"server":    s.cfg.EmbedServer,
			"model":     s.cfg.EmbedModel,
			"reachable": s.embedderReachable(r.Context()),
		},
	})
}

// embedderReachable does a short, best-effort GET against the Ollama
// server's tag-list endpoint. Returns false on any error / timeout so a
// down embedding server never hangs the dashboard.
func (s *Server) embedderReachable(ctx context.Context) bool {
	if s.cfg.EmbedServer == "" {
		return false
	}
	pingCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	url := strings.TrimRight(s.cfg.EmbedServer, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(pingCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}

// --- /healthz ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"status":         "ok",
		"uptime_seconds": time.Since(s.startedAt).Seconds(),
		"index_open":     s.cfg.Index != nil,
	})
}

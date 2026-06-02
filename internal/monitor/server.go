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
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Version             string
	Mode                string // "mcp-stdio" | "mcp-http" | "mcp-sse" | "watch"
	Index               index.Index
	Collector           *Collector
	EmbedServer         string
	EmbedModel          string
	IndexPath           string // "" → in-memory
	IndexBackend        string // "persistent" | "in-memory"
	IndexFallbackReason string // "" | "lock_contention" | "no_index_flag"
	BodyCacheCap        int64  // 0 → in-memory / no cap

	// Cwd is the server's working directory at start; warm endpoints
	// default to walking it when the caller doesn't pin a dir.
	Cwd string

	// WarmAttrsFn / WarmBodyFn / WarmEmbeddingsFn are dependency-injected
	// from cmd/file-search-on so the monitor package doesn't import the
	// main-package warmers. Each is called from a fire-and-forget
	// goroutine spawned by the matching POST handler. Nil indicates the
	// feature isn't wired (handler returns 412 Precondition Failed).
	WarmAttrsFn      func(ctx context.Context, root string) error
	WarmBodyFn       func(ctx context.Context, root string) error
	WarmEmbeddingsFn func(ctx context.Context, root string) error
}

// warmingState is the snapshot returned via /api/overview while a warm
// goroutine is running. The Server stores it in a sync/atomic.Pointer so
// the GET path is lock-free.
type warmingState struct {
	Kind         string    `json:"kind"` // "attrs" | "bodies" | "embeddings"
	Root         string    `json:"root"`
	StartedAt    time.Time `json:"started_at"`
	LastDuration string    `json:"last_duration,omitempty"`
	LastKind     string    `json:"last_kind,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
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

	// warming is the live state of the most-recent (or in-flight) warm
	// goroutine — nil when none has ever run, or set to a state with
	// LastDuration filled in once one has completed. /api/overview
	// reads via atomic.Pointer.Load.
	warming atomic.Pointer[warmingState]

	// warmMu serialises POSTs to the warm endpoints so two concurrent
	// clicks can't both spawn goroutines that race on the same root.
	// The lock is only held during the in-flight start handshake; the
	// goroutine body runs unlocked.
	warmMu sync.Mutex

	// bus fans Collector.Record events out to every connected SSE
	// client. Initialised by NewServer; the listener is attached the
	// first time routes() runs so it's safe to construct a Server
	// without a Collector (watch-mode dashboard).
	bus *eventBus
}

// NewServer builds a monitoring server from cfg.
func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg, startedAt: time.Now(), bus: newEventBus()}
	if cfg.Collector != nil {
		// Synchronous fan-out: the bus.Publish path is a non-blocking
		// buffered-chan send with a drop-oldest fallback, so attaching
		// this listener can't slow Collector.Record down.
		cfg.Collector.AddListener(func(rec CallRecord) {
			s.bus.Publish(streamEvent{Kind: "activity", Data: rec})
		})
	}
	return s
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
	mux.HandleFunc("GET /api/cache/entries", s.handleCacheEntries)
	mux.HandleFunc("GET /api/cache/entry", s.handleCacheEntry)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	// Mutating endpoints. Reachable only via the 127.0.0.1 binding;
	// the operator started the file-search-on process that owns the
	// cache, and the browser same-origin policy blocks cross-origin
	// POSTs. No auth token / CSRF check — the loopback binding IS
	// the trust boundary. See plan doc + ADR linked in CHANGELOG.
	mux.HandleFunc("POST /api/cache/evict", s.handleCacheEvict)
	mux.HandleFunc("POST /api/cache/clear", s.handleCacheClear)
	mux.HandleFunc("POST /api/cache/warm-attrs", s.handleWarmAttrs)
	mux.HandleFunc("POST /api/cache/warm-bodies", s.handleWarmBodies)
	mux.HandleFunc("POST /api/cache/warm-embeddings", s.handleWarmEmbeddings)
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
		PID:                 os.Getpid(),
		URL:                 s.url,
		Mode:                s.cfg.Mode,
		Version:             s.cfg.Version,
		StartedAt:           s.startedAt,
		WorkingDir:          workingDir(),
		IndexPath:           s.cfg.IndexPath,
		IndexBackend:        s.cfg.IndexBackend,
		IndexFallbackReason: s.cfg.IndexFallbackReason,
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

// overviewPayload assembles the JSON-able snapshot used by both
// /api/overview and the SSE heartbeat. Kept as a method on Server so
// the warming-state pointer + config are in scope.
func (s *Server) overviewPayload() map[string]any {
	indexBacking := "in-memory"
	if s.cfg.IndexPath != "" {
		indexBacking = s.cfg.IndexPath
	}
	payload := map[string]any{
		"version":               s.cfg.Version,
		"mode":                  s.cfg.Mode,
		"uptime_seconds":        time.Since(s.startedAt).Seconds(),
		"pid":                   os.Getpid(),
		"go_version":            runtime.Version(),
		"gomaxprocs":            runtime.GOMAXPROCS(0),
		"num_cpu":               runtime.NumCPU(),
		"index_backing":         indexBacking,
		"index_backend":         s.cfg.IndexBackend,
		"index_path":            s.cfg.IndexPath,
		"index_fallback_reason": s.cfg.IndexFallbackReason,
		"body_cache_cap":        s.cfg.BodyCacheCap,
		"goroutines":            runtime.NumGoroutine(),
		"cwd":                       s.cfg.Cwd,
		"warm_attrs_available":      s.cfg.WarmAttrsFn != nil,
		"warm_bodies_available":     s.cfg.WarmBodyFn != nil,
		"warm_embeddings_available": s.cfg.WarmEmbeddingsFn != nil,
	}
	if state := s.warming.Load(); state != nil {
		payload["warming"] = state
	}
	return payload
}

func (s *Server) handleOverview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.overviewPayload())
}

// --- /api/cache ---

// cachePayload returns the JSON-able cache snapshot used by both
// /api/cache and the SSE heartbeat. Returns {"available": false} when
// no index is attached so the UI's empty-state branches match.
func (s *Server) cachePayload() map[string]any {
	if s.cfg.Index == nil {
		return map[string]any{"available": false}
	}
	st := s.cfg.Index.Stats()
	return map[string]any{
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
		"attr_entries_count": st.AttrEntriesCount,
		"body_entries_count": st.BodyEntriesCount,
		"bodies_total_bytes": st.BodiesTotalBytes,
	}
}

func (s *Server) handleCache(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.cachePayload())
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

// activityPayload returns the JSON-able activity snapshot used by both
// /api/activity and the SSE heartbeat. Reports unavailable when there's
// no collector (the watch-mode dashboard).
func (s *Server) activityPayload() map[string]any {
	if s.cfg.Collector == nil {
		return map[string]any{
			"available": false,
			"reason":    "no MCP activity in this mode",
		}
	}
	return map[string]any{
		"available": true,
		"snapshot":  s.cfg.Collector.Snapshot(),
	}
}

func (s *Server) handleActivity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.activityPayload())
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

// --- /api/cache/entries — paginated list ---

// cacheEntriesLimitCap is the server-side hard ceiling on the list
// response size. Callers that ask for more get this many; callers
// who don't specify get cacheEntriesDefaultLimit.
const (
	cacheEntriesDefaultLimit = 50
	cacheEntriesLimitCap     = 500
)

func (s *Server) handleCacheEntries(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Index == nil {
		writeJSON(w, map[string]any{"available": false})
		return
	}
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket != "attrs" && bucket != "bodies" {
		http.Error(w, "bucket must be 'attrs' or 'bodies'", http.StatusBadRequest)
		return
	}
	substr := q.Get("q")
	limit := min(parseIntDefault(q.Get("limit"), cacheEntriesDefaultLimit), cacheEntriesLimitCap)
	offset := parseIntDefault(q.Get("offset"), 0)

	switch bucket {
	case "attrs":
		entries, total, err := s.cfg.Index.ListAttrs(substr, limit, offset)
		if err != nil {
			http.Error(w, fmt.Sprintf("list attrs: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"bucket":  "attrs",
			"q":       substr,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
			"entries": entries,
		})
	case "bodies":
		entries, total, err := s.cfg.Index.ListBodies(substr, limit, offset)
		if err != nil {
			http.Error(w, fmt.Sprintf("list bodies: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"bucket":  "bodies",
			"q":       substr,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
			"entries": entries,
		})
	}
}

// --- /api/cache/entry — single entry detail ---

// cacheBodyDetailMaxBytes caps a single body-detail response to
// keep the dashboard responsive even on huge bodies. Larger bodies
// surface as truncated=true with the prefix.
const cacheBodyDetailMaxBytes = 64 * 1024

func (s *Server) handleCacheEntry(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Index == nil {
		writeJSON(w, map[string]any{"available": false})
		return
	}
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket != "attrs" && bucket != "bodies" {
		http.Error(w, "bucket must be 'attrs' or 'bodies'", http.StatusBadRequest)
		return
	}
	path := q.Get("path")
	if err := validateAbsPath(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch bucket {
	case "attrs":
		e, ok := s.cfg.Index.PeekAttrs(path)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Staleness for individual entries is surfaced on the list
		// view (which walks the bucket cursor and stats each row with
		// path-as-cache-key, not as user input). The detail view
		// deliberately does NOT os.Stat the user-supplied path —
		// even though the dashboard binds 127.0.0.1 only, the
		// pattern is bad hygiene (CodeQL go/path-injection) and a
		// future "expose via proxy" feature would inherit it.
		// Users wanting current staleness for a specific path
		// refresh the list view.
		writeJSON(w, map[string]any{
			"bucket":       "attrs",
			"path":         path,
			"content_type": e.ContentType,
			"size":         e.Size,
			"mod_time":     time.Unix(0, e.ModTimeUnixNano),
			"extra":        e.Extra,
			"hash":         e.Hash,
			"md5":          e.MD5,
			"sha1":         e.SHA1,
			"embed_model":  e.EmbedModel,
			"has_vector":   len(e.Vector) > 0,
			"vector_dims":  len(e.Vector),
		})
	case "bodies":
		be, ok := s.cfg.Index.PeekBody(path)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body := be.Body
		truncated := false
		if len(body) > cacheBodyDetailMaxBytes {
			body = body[:cacheBodyDetailMaxBytes]
			truncated = true
		}
		writeJSON(w, map[string]any{
			"bucket":      "bodies",
			"path":        path,
			"size":        be.Size,
			"mod_time":    time.Unix(0, be.ModTimeUnixNano),
			"created_at":  time.Unix(0, be.CreatedUnixNano),
			"body":        body,
			"body_length": len(be.Body),
			"truncated":   truncated,
		})
	}
}

// validateAbsPath enforces the same shape PR #249 chose for cache
// browsing: absolute, filepath.Clean()-normalised. Both Put callers
// (the walker) only ever store keys that already match this, so any
// non-conforming path can't hit a cache entry anyway. Validating up
// front keeps the handler off CodeQL's go/path-injection radar.
func validateAbsPath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("path must be an absolute, clean filesystem path")
	}
	return nil
}

// --- POST /api/cache/evict ---

func (s *Server) handleCacheEvict(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Index == nil {
		http.Error(w, "index not available", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := r.Form.Get("path")
	if err := validateAbsPath(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.cfg.Index.Delete(path); err != nil {
		http.Error(w, fmt.Sprintf("delete: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- POST /api/cache/clear ---

func (s *Server) handleCacheClear(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Index == nil {
		http.Error(w, "index not available", http.StatusServiceUnavailable)
		return
	}
	if err := s.cfg.Index.Clear(); err != nil {
		http.Error(w, fmt.Sprintf("clear: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- POST /api/cache/warm-* (shared plumbing) ---

// warmRequest extracts the root dir from the request form, falling back
// to cfg.Cwd. Validates the result as an absolute clean path.
func (s *Server) warmRequest(r *http.Request) (string, error) {
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	root := r.Form.Get("dir")
	if root == "" {
		root = s.cfg.Cwd
	}
	if err := validateAbsPath(root); err != nil {
		return "", err
	}
	return root, nil
}

// startWarm wraps the warm goroutine bookkeeping: rejects if another
// warm is already in flight (HTTP 409), publishes the in-flight state
// for /api/overview, and clears it (storing the post-mortem fields) on
// completion. fn must return promptly when ctx is cancelled.
//
// The goroutine uses a fresh context.Background() rather than the
// request context — operators want the warm to keep running after
// their POST returns 202. The parent ctx the Server was started with
// would be ideal, but Serve doesn't currently expose it; in practice
// SIGINT / SIGTERM kills the whole process, which tears the goroutine
// down with it. A 30-minute timeout caps the worst case.
func (s *Server) startWarm(w http.ResponseWriter, kind, root string, fn func(ctx context.Context, root string) error) {
	s.warmMu.Lock()
	if cur := s.warming.Load(); cur != nil && cur.LastDuration == "" {
		s.warmMu.Unlock()
		http.Error(w, fmt.Sprintf("a %s warm is already in flight", cur.Kind), http.StatusConflict)
		return
	}
	state := &warmingState{Kind: kind, Root: root, StartedAt: time.Now()}
	s.warming.Store(state)
	s.warmMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := fn(ctx, root)
		done := &warmingState{
			Kind:         "", // empty => idle
			Root:         root,
			StartedAt:    state.StartedAt,
			LastKind:     kind,
			LastDuration: time.Since(state.StartedAt).Round(time.Millisecond).String(),
		}
		if err != nil {
			done.LastError = err.Error()
		}
		s.warming.Store(done)
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"kind": kind, "root": root, "started_at": state.StartedAt})
}

// --- POST /api/cache/warm-attrs ---

func (s *Server) handleWarmAttrs(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WarmAttrsFn == nil {
		http.Error(w, "warm-attrs not wired (Cwd / WarmAttrsFn missing)", http.StatusPreconditionFailed)
		return
	}
	root, err := s.warmRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.startWarm(w, "attrs", root, s.cfg.WarmAttrsFn)
}

// --- POST /api/cache/warm-bodies ---

func (s *Server) handleWarmBodies(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WarmBodyFn == nil {
		http.Error(w, "warm-bodies not wired (Cwd / WarmBodyFn missing)", http.StatusPreconditionFailed)
		return
	}
	root, err := s.warmRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.startWarm(w, "bodies", root, s.cfg.WarmBodyFn)
}

// --- POST /api/cache/warm-embeddings ---

func (s *Server) handleWarmEmbeddings(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WarmEmbeddingsFn == nil {
		http.Error(w, "warm-embeddings not wired — set --embedding-model on the server", http.StatusPreconditionFailed)
		return
	}
	root, err := s.warmRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.startWarm(w, "embeddings", root, s.cfg.WarmEmbeddingsFn)
}

// parseIntDefault parses s as int; returns def on error / empty.
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

package index

import (
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type memoryIndex struct {
	mu     sync.RWMutex
	data   map[string]*Entry
	bodies map[string]*BodyEntry
	stats  memoryStats
}

type memoryStats struct {
	hits          atomic.Uint64
	misses        atomic.Uint64
	puts          atomic.Uint64
	stales        atomic.Uint64
	errors        atomic.Uint64
	entryOversize atomic.Uint64 // Put dropped: encoded Entry > maxEntryBytes (#346/#348)

	bodyHits      atomic.Uint64
	bodyMisses    atomic.Uint64
	bodyPuts      atomic.Uint64
	bodyStales    atomic.Uint64
	bodyEvictions atomic.Uint64
	bodyOversize  atomic.Uint64
	bodyErrors    atomic.Uint64

	embedHits            atomic.Uint64
	embedMisses          atomic.Uint64
	embedPuts            atomic.Uint64
	embedErrors          atomic.Uint64
	embedModelMismatches atomic.Uint64
}

func newMemoryIndex() *memoryIndex {
	return &memoryIndex{
		data:   make(map[string]*Entry),
		bodies: make(map[string]*BodyEntry),
	}
}

func (m *memoryIndex) Lookup(path string, size int64, mtime time.Time) (*Entry, bool) {
	if path == "" || mtime.IsZero() {
		m.stats.misses.Add(1)
		return nil, false
	}
	m.mu.RLock()
	e, ok := m.data[path]
	m.mu.RUnlock()
	if !ok {
		m.stats.misses.Add(1)
		return nil, false
	}
	if e.Size != size || e.ModTimeUnixNano != mtime.UnixNano() {
		m.stats.stales.Add(1)
		return nil, false
	}
	m.stats.hits.Add(1)
	return e, true
}

func (m *memoryIndex) Put(path string, e *Entry) error {
	if path == "" || e == nil {
		m.stats.errors.Add(1)
		return nil
	}
	m.mu.Lock()
	m.data[path] = e
	m.mu.Unlock()
	m.stats.puts.Add(1)
	return nil
}

// LookupBody mirrors Lookup against the body sub-map. In-memory has
// no eviction — process lifetime bounds storage. (size, mtime)
// validation is identical to Lookup so attribute and body cache
// invalidate together on file change.
func (m *memoryIndex) LookupBody(path string, size int64, mtime time.Time) (string, bool) {
	if path == "" || mtime.IsZero() {
		m.stats.bodyMisses.Add(1)
		return "", false
	}
	m.mu.RLock()
	be, ok := m.bodies[path]
	m.mu.RUnlock()
	if !ok {
		m.stats.bodyMisses.Add(1)
		return "", false
	}
	if be.Size != size || be.ModTimeUnixNano != mtime.UnixNano() {
		m.stats.bodyStales.Add(1)
		return "", false
	}
	m.stats.bodyHits.Add(1)
	return be.Body, true
}

// PutBody stores a body in the in-memory map. No size cap or eviction
// — agents using the in-memory index typically run short MCP sessions
// where memory pressure is bounded by walk lifetime.
func (m *memoryIndex) PutBody(path string, be *BodyEntry) error {
	if path == "" || be == nil {
		m.stats.bodyErrors.Add(1)
		return nil
	}
	m.mu.Lock()
	m.bodies[path] = be
	m.mu.Unlock()
	m.stats.bodyPuts.Add(1)
	return nil
}

func (m *memoryIndex) Stats() Stats {
	m.mu.RLock()
	attrCount := uint64(len(m.data))
	bodyCount := uint64(len(m.bodies))
	m.mu.RUnlock()
	return Stats{
		Hits:                 m.stats.hits.Load(),
		Misses:               m.stats.misses.Load(),
		Puts:                 m.stats.puts.Load(),
		Stales:               m.stats.stales.Load(),
		Errors:               m.stats.errors.Load(),
		EntryOversize:        m.stats.entryOversize.Load(),
		BodyHits:             m.stats.bodyHits.Load(),
		BodyMisses:           m.stats.bodyMisses.Load(),
		BodyPuts:             m.stats.bodyPuts.Load(),
		BodyStales:           m.stats.bodyStales.Load(),
		BodyEvictions:        m.stats.bodyEvictions.Load(),
		BodyOversize:         m.stats.bodyOversize.Load(),
		BodyErrors:           m.stats.bodyErrors.Load(),
		EmbedHits:            m.stats.embedHits.Load(),
		EmbedMisses:          m.stats.embedMisses.Load(),
		EmbedPuts:            m.stats.embedPuts.Load(),
		EmbedErrors:          m.stats.embedErrors.Load(),
		EmbedModelMismatches: m.stats.embedModelMismatches.Load(),
		AttrEntriesCount:     attrCount,
		BodyEntriesCount:     bodyCount,
	}
}

// ListAttrs iterates m.data, filters by substring, sorts by path,
// and slices for pagination. Each row's Stale flag is computed by
// stat'ing the live file (cheap for the typical browser page size).
func (m *memoryIndex) ListAttrs(substr string, limit, offset int) ([]EntrySummary, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matches []string
	for k := range m.data {
		if substr == "" || strings.Contains(k, substr) {
			matches = append(matches, k)
		}
	}
	sort.Strings(matches)
	total := len(matches)
	from := min(offset, total)
	to := min(from+limit, total)
	out := make([]EntrySummary, 0, to-from)
	for _, p := range matches[from:to] {
		e := m.data[p]
		out = append(out, EntrySummary{
			Path:        p,
			ContentType: e.ContentType,
			Size:        e.Size,
			ModTime:     time.Unix(0, e.ModTimeUnixNano),
			Stale:       isAttrStaleEntry(p, e),
		})
	}
	return out, total, nil
}

// ListBodies is the body-cache analogue of ListAttrs. LastAccess is
// always zero — the in-memory backend has no FIFO eviction layer.
func (m *memoryIndex) ListBodies(substr string, limit, offset int) ([]BodySummary, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matches []string
	for k := range m.bodies {
		if substr == "" || strings.Contains(k, substr) {
			matches = append(matches, k)
		}
	}
	sort.Strings(matches)
	total := len(matches)
	from := min(offset, total)
	to := min(from+limit, total)
	out := make([]BodySummary, 0, to-from)
	for _, p := range matches[from:to] {
		be := m.bodies[p]
		out = append(out, BodySummary{
			Path:    p,
			Size:    be.Size,
			ModTime: time.Unix(0, be.ModTimeUnixNano),
			Stale:   isBodyStaleEntry(p, be),
		})
	}
	return out, total, nil
}

// PeekAttrs returns m.data[absPath] bypassing validation.
func (m *memoryIndex) PeekAttrs(absPath string) (*Entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.data[absPath]
	return e, ok
}

// PeekBody returns m.bodies[absPath] bypassing validation.
func (m *memoryIndex) PeekBody(absPath string) (*BodyEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	be, ok := m.bodies[absPath]
	return be, ok
}

// Delete removes attribute + body entries for absPath. Idempotent —
// returns nil even when no entry was cached. The Stats counters are
// monotonic-since-process-start and are NOT decremented.
func (m *memoryIndex) Delete(absPath string) error {
	if absPath == "" {
		return nil
	}
	m.mu.Lock()
	delete(m.data, absPath)
	delete(m.bodies, absPath)
	m.mu.Unlock()
	return nil
}

// Clear wipes every cached entry across attrs + bodies. Re-inits the
// maps so subsequent Puts see a fresh state. Stats counters stay
// monotonic-since-process-start (reset only on process restart) so a
// "cleared the cache, what's my hit-rate trend" question still works.
func (m *memoryIndex) Clear() error {
	m.mu.Lock()
	m.data = make(map[string]*Entry)
	m.bodies = make(map[string]*BodyEntry)
	m.mu.Unlock()
	return nil
}

// isAttrStaleEntry and isBodyStaleEntry are the shared
// stat-and-compare used by ListAttrs / ListBodies across both
// backends. (bbolt.go has type-specific copies because it can't
// import from a separate file without a re-org — duplication is
// minimal and the helpers don't share state.)
func isAttrStaleEntry(p string, e *Entry) bool {
	info, err := os.Stat(p)
	if err != nil {
		return true
	}
	return info.Size() != e.Size || info.ModTime().UnixNano() != e.ModTimeUnixNano
}

func isBodyStaleEntry(p string, be *BodyEntry) bool {
	info, err := os.Stat(p)
	if err != nil {
		return true
	}
	return info.Size() != be.Size || info.ModTime().UnixNano() != be.ModTimeUnixNano
}

func (m *memoryIndex) BumpEmbedStat(kind string) { bumpEmbedStat(&m.stats, kind) }

// bumpEmbedStat is shared between the in-memory and bbolt backends —
// the embedding counters live on memoryStats either way (the bbolt
// impl reuses the same struct via embedding).
func bumpEmbedStat(s *memoryStats, kind string) {
	switch kind {
	case "hit":
		s.embedHits.Add(1)
	case "miss":
		s.embedMisses.Add(1)
	case "put":
		s.embedPuts.Add(1)
	case "error":
		s.embedErrors.Add(1)
	case "model_mismatch":
		s.embedModelMismatches.Add(1)
	}
}

func (m *memoryIndex) Close() error { return nil }

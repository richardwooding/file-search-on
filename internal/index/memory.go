package index

import (
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
	hits   atomic.Uint64
	misses atomic.Uint64
	puts   atomic.Uint64
	stales atomic.Uint64
	errors atomic.Uint64

	bodyHits      atomic.Uint64
	bodyMisses    atomic.Uint64
	bodyPuts      atomic.Uint64
	bodyStales    atomic.Uint64
	bodyEvictions atomic.Uint64
	bodyOversize  atomic.Uint64
	bodyErrors    atomic.Uint64

	embedHits   atomic.Uint64
	embedMisses atomic.Uint64
	embedPuts   atomic.Uint64
	embedErrors atomic.Uint64
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
	return Stats{
		Hits:          m.stats.hits.Load(),
		Misses:        m.stats.misses.Load(),
		Puts:          m.stats.puts.Load(),
		Stales:        m.stats.stales.Load(),
		Errors:        m.stats.errors.Load(),
		BodyHits:      m.stats.bodyHits.Load(),
		BodyMisses:    m.stats.bodyMisses.Load(),
		BodyPuts:      m.stats.bodyPuts.Load(),
		BodyStales:    m.stats.bodyStales.Load(),
		BodyEvictions: m.stats.bodyEvictions.Load(),
		BodyOversize:  m.stats.bodyOversize.Load(),
		BodyErrors:    m.stats.bodyErrors.Load(),
		EmbedHits:     m.stats.embedHits.Load(),
		EmbedMisses:   m.stats.embedMisses.Load(),
		EmbedPuts:     m.stats.embedPuts.Load(),
		EmbedErrors:   m.stats.embedErrors.Load(),
	}
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
	}
}

func (m *memoryIndex) Close() error { return nil }

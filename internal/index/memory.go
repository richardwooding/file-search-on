package index

import (
	"sync"
	"sync/atomic"
	"time"
)

type memoryIndex struct {
	mu    sync.RWMutex
	data  map[string]*Entry
	stats memoryStats
}

type memoryStats struct {
	hits   atomic.Uint64
	misses atomic.Uint64
	puts   atomic.Uint64
	stales atomic.Uint64
	errors atomic.Uint64
}

func newMemoryIndex() *memoryIndex {
	return &memoryIndex{data: make(map[string]*Entry)}
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

func (m *memoryIndex) Stats() Stats {
	return Stats{
		Hits:   m.stats.hits.Load(),
		Misses: m.stats.misses.Load(),
		Puts:   m.stats.puts.Load(),
		Stales: m.stats.stales.Load(),
		Errors: m.stats.errors.Load(),
	}
}

func (m *memoryIndex) Close() error { return nil }

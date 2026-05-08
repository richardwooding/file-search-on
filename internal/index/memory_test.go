package index

import (
	"sync"
	"testing"
	"time"
)

func TestMemoryHitMissStale(t *testing.T) {
	idx := NewMemory()
	defer func() { _ = idx.Close() }()

	mtime := time.Unix(1700000000, 0)
	e := &Entry{
		Size:            10,
		ModTimeUnixNano: mtime.UnixNano(),
		ContentType:     "markdown",
		Extra:           map[string]any{"word_count": int64(3)},
	}
	if err := idx.Put("/abs/a.md", e); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Hit
	if got, ok := idx.Lookup("/abs/a.md", 10, mtime); !ok {
		t.Fatalf("expected hit")
	} else if got.ContentType != "markdown" {
		t.Errorf("got=%v want markdown", got.ContentType)
	}

	// Miss (no key)
	if _, ok := idx.Lookup("/abs/missing.md", 10, mtime); ok {
		t.Errorf("expected miss for absent key")
	}

	// Stale via mtime
	if _, ok := idx.Lookup("/abs/a.md", 10, mtime.Add(time.Second)); ok {
		t.Errorf("expected stale on mtime mismatch")
	}
	// Stale via size
	if _, ok := idx.Lookup("/abs/a.md", 11, mtime); ok {
		t.Errorf("expected stale on size mismatch")
	}

	// Zero mtime never hits and never bumps unrelated counters into hits/stales.
	if _, ok := idx.Lookup("/abs/a.md", 10, time.Time{}); ok {
		t.Errorf("zero mtime must not hit")
	}

	st := idx.Stats()
	if st.Hits != 1 {
		t.Errorf("Hits=%d want 1", st.Hits)
	}
	if st.Stales != 2 {
		t.Errorf("Stales=%d want 2", st.Stales)
	}
	if st.Puts != 1 {
		t.Errorf("Puts=%d want 1", st.Puts)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	idx := NewMemory()
	defer func() { _ = idx.Close() }()
	mtime := time.Unix(1700000000, 0)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := "/abs/file"
			if id%2 == 0 {
				_ = idx.Put(path, &Entry{Size: int64(id), ModTimeUnixNano: mtime.UnixNano()})
			} else {
				_, _ = idx.Lookup(path, int64(id), mtime)
			}
		}(i)
	}
	wg.Wait()
	// Just ensure no panics under -race; counters are best-effort here.
	_ = idx.Stats()
}

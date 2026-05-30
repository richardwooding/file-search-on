package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedAttrs writes len(paths) entries into idx with deterministic
// content. Each path actually exists on disk (in tmp) so isAttrStale
// returns false for fresh entries. Returns the tmp dir and the
// absolute paths in declaration order.
func seedAttrs(t *testing.T, idx Index, basenames ...string) (string, []string) {
	t.Helper()
	tmp := t.TempDir()
	paths := make([]string, 0, len(basenames))
	for _, b := range basenames {
		p := filepath.Join(tmp, b)
		if err := os.WriteFile(p, []byte("body of "+b), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if err := idx.Put(p, &Entry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			ContentType:     "text",
			Extra:           map[string]any{"basename": b},
		}); err != nil {
			t.Fatalf("Put %s: %v", p, err)
		}
		paths = append(paths, p)
	}
	return tmp, paths
}

func seedBodies(t *testing.T, idx Index, basenames ...string) (string, []string) {
	t.Helper()
	tmp := t.TempDir()
	paths := make([]string, 0, len(basenames))
	for _, b := range basenames {
		p := filepath.Join(tmp, b)
		body := "body of " + b + "\n"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if err := idx.PutBody(p, &BodyEntry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			CreatedUnixNano: time.Now().UnixNano(),
			Body:            body,
		}); err != nil {
			t.Fatalf("PutBody %s: %v", p, err)
		}
		paths = append(paths, p)
	}
	return tmp, paths
}

// openTestIndex returns both backends; tests run their assertions
// against each. The bbolt backend's writer is async; flushAttrs
// blocks until the put has landed.
func openTestIndex(t *testing.T, name string) Index {
	t.Helper()
	if name == "memory" {
		return NewMemory()
	}
	path := filepath.Join(t.TempDir(), "test.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// flushAttrs forces the bbolt writer to drain by issuing a Lookup
// for a known path (which acquires the read lock and synchronises
// with the writer's batch commit). For the memory backend it's a
// no-op since Put is synchronous.
func flushAttrs(t *testing.T, idx Index, knownPath string) {
	t.Helper()
	// The bbolt writer batches every 100ms; wait at least one batch
	// cycle to be sure pending Puts landed before we List them.
	for range 50 {
		if _, ok := idx.Lookup(knownPath, mustStatSize(t, knownPath), mustStatMtime(t, knownPath)); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("flushAttrs: Put for %s did not become visible within 500ms", knownPath)
}

func mustStatSize(t *testing.T, p string) int64 {
	t.Helper()
	i, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return i.Size()
}

func mustStatMtime(t *testing.T, p string) time.Time {
	t.Helper()
	i, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return i.ModTime()
}

func TestListAttrs_PaginationAndFilter(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedAttrs(t, idx, "alpha.md", "beta.md", "gamma.md", "delta.txt", "epsilon.md")
			if backend == "bbolt" {
				flushAttrs(t, idx, paths[len(paths)-1])
			}

			// No filter, full list — should return all 5 sorted lexicographically.
			got, total, err := idx.ListAttrs("", 50, 0)
			if err != nil {
				t.Fatalf("ListAttrs: %v", err)
			}
			if total != 5 {
				t.Errorf("total = %d, want 5", total)
			}
			if len(got) != 5 {
				t.Errorf("entries len = %d, want 5", len(got))
			}
			// Filter by ".md" — alpha/beta/gamma/epsilon = 4 hits.
			_, total, err = idx.ListAttrs(".md", 50, 0)
			if err != nil {
				t.Fatalf("ListAttrs filter: %v", err)
			}
			if total != 4 {
				t.Errorf("filtered total = %d, want 4", total)
			}
			// Pagination — limit 2, offset 1.
			got, _, err = idx.ListAttrs("", 2, 1)
			if err != nil {
				t.Fatalf("ListAttrs page: %v", err)
			}
			if len(got) != 2 {
				t.Errorf("paged len = %d, want 2", len(got))
			}
		})
	}
}

func TestListAttrs_StaleFlagSet(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedAttrs(t, idx, "a.md")
			if backend == "bbolt" {
				flushAttrs(t, idx, paths[0])
			}

			// Initially fresh — Stale should be false.
			got, _, err := idx.ListAttrs("", 50, 0)
			if err != nil {
				t.Fatalf("ListAttrs: %v", err)
			}
			if got[0].Stale {
				t.Errorf("fresh entry reported as stale: %+v", got[0])
			}

			// Mutate the file so size/mtime drift from the cached entry.
			if err := os.WriteFile(paths[0], []byte("CHANGED CONTENT MORE BYTES"), 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			got, _, err = idx.ListAttrs("", 50, 0)
			if err != nil {
				t.Fatalf("ListAttrs re-list: %v", err)
			}
			if !got[0].Stale {
				t.Errorf("expected Stale=true after rewrite, got %+v", got[0])
			}
		})
	}
}

func TestPeekAttrs_ReturnsStale(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedAttrs(t, idx, "a.md")
			if backend == "bbolt" {
				flushAttrs(t, idx, paths[0])
			}

			// PeekAttrs should return the entry regardless of staleness.
			if err := os.WriteFile(paths[0], []byte("CHANGED"), 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			e, ok := idx.PeekAttrs(paths[0])
			if !ok {
				t.Fatalf("PeekAttrs returned !ok for a known-cached entry")
			}
			if e.ContentType != "text" {
				t.Errorf("Entry.ContentType = %q, want %q", e.ContentType, "text")
			}

			// Lookup (strict) should now fail with stale.
			if _, ok := idx.Lookup(paths[0], 7, time.Now()); ok {
				t.Errorf("Lookup unexpectedly succeeded after rewrite")
			}
		})
	}
}

func TestPeekAttrs_MissingPath(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			if _, ok := idx.PeekAttrs("/nonexistent/path"); ok {
				t.Errorf("PeekAttrs returned ok=true for unknown path")
			}
		})
	}
}

func TestListBodies_FilterAndPagination(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedBodies(t, idx, "a.md", "b.md", "c.txt")
			if backend == "bbolt" {
				// Body writer is also async; wait for the body to be queryable.
				for range 50 {
					if _, ok := idx.LookupBody(paths[0], mustStatSize(t, paths[0]), mustStatMtime(t, paths[0])); ok {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}

			got, total, err := idx.ListBodies(".md", 50, 0)
			if err != nil {
				t.Fatalf("ListBodies: %v", err)
			}
			if total != 2 {
				t.Errorf("filtered total = %d, want 2 (.md only)", total)
			}
			if len(got) != 2 {
				t.Errorf("entries len = %d, want 2", len(got))
			}
		})
	}
}

func TestPeekBody_ReturnsBody(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedBodies(t, idx, "doc.md")
			if backend == "bbolt" {
				for range 50 {
					if _, ok := idx.LookupBody(paths[0], mustStatSize(t, paths[0]), mustStatMtime(t, paths[0])); ok {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			be, ok := idx.PeekBody(paths[0])
			if !ok {
				t.Fatalf("PeekBody returned !ok for a known-cached body")
			}
			if be.Body == "" {
				t.Errorf("Body is empty; want 'body of doc.md\\n'")
			}
		})
	}
}

func TestStats_EntryCounts(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedAttrs(t, idx, "a.md", "b.md", "c.md")
			if backend == "bbolt" {
				flushAttrs(t, idx, paths[len(paths)-1])
			}
			s := idx.Stats()
			if s.AttrEntriesCount != 3 {
				t.Errorf("AttrEntriesCount = %d, want 3", s.AttrEntriesCount)
			}
		})
	}
}

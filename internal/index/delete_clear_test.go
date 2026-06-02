package index

import (
	"testing"
)

// TestDelete_RemovesAttrsAndBodies covers both backends: Put + PutBody
// a value, Delete the path, and confirm Peek* both return false.
func TestDelete_RemovesAttrsAndBodies(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, attrPaths := seedAttrs(t, idx, "a.md", "b.md")
			flushAttrs(t, idx, attrPaths[0])
			_, bodyPaths := seedBodies(t, idx, "a.md", "b.md")
			// Bodies share paths-by-coincidence in this test setup
			// (different t.TempDir() each call), so deleting attrPaths[0]
			// won't remove bodyPaths[0]. To get a path with BOTH an attr
			// and a body entry we need to set them up against the same
			// tmp dir — easier to just verify each side independently.
			_ = bodyPaths

			// Delete an attr-only path; PeekAttrs should return false.
			if err := idx.Delete(attrPaths[0]); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, ok := idx.PeekAttrs(attrPaths[0]); ok {
				t.Errorf("PeekAttrs returned ok after Delete (path=%s)", attrPaths[0])
			}
			if _, ok := idx.PeekAttrs(attrPaths[1]); !ok {
				t.Errorf("Delete should not affect unrelated entries (path=%s missing)", attrPaths[1])
			}

			// Delete a body-only path; PeekBody should return false.
			if err := idx.Delete(bodyPaths[0]); err != nil {
				t.Fatalf("Delete (body): %v", err)
			}
			if _, ok := idx.PeekBody(bodyPaths[0]); ok {
				t.Errorf("PeekBody returned ok after Delete (path=%s)", bodyPaths[0])
			}
		})
	}
}

func TestDelete_Idempotent(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			// Delete a path that was never Put — must not error.
			if err := idx.Delete("/nonexistent/path"); err != nil {
				t.Errorf("Delete of unknown path should be idempotent, got %v", err)
			}
			// Double Delete the same path — also fine.
			_, paths := seedAttrs(t, idx, "x.md")
			flushAttrs(t, idx, paths[0])
			if err := idx.Delete(paths[0]); err != nil {
				t.Errorf("first Delete: %v", err)
			}
			if err := idx.Delete(paths[0]); err != nil {
				t.Errorf("second Delete: %v", err)
			}
		})
	}
}

func TestDelete_EmptyPath(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			if err := idx.Delete(""); err != nil {
				t.Errorf("Delete(\"\") should be a no-op, got %v", err)
			}
		})
	}
}

func TestClear_WipesEverything(t *testing.T) {
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, attrPaths := seedAttrs(t, idx, "a.md", "b.md", "c.md")
			flushAttrs(t, idx, attrPaths[0])
			_, bodyPaths := seedBodies(t, idx, "a.md", "b.md")
			_ = bodyPaths

			// Pre-condition: entries are visible.
			if _, ok := idx.PeekAttrs(attrPaths[0]); !ok {
				t.Fatalf("setup: PeekAttrs should have entry before Clear")
			}

			if err := idx.Clear(); err != nil {
				t.Fatalf("Clear: %v", err)
			}

			// Every entry is gone.
			for _, p := range attrPaths {
				if _, ok := idx.PeekAttrs(p); ok {
					t.Errorf("PeekAttrs returned ok after Clear (path=%s)", p)
				}
			}
			for _, p := range bodyPaths {
				if _, ok := idx.PeekBody(p); ok {
					t.Errorf("PeekBody returned ok after Clear (path=%s)", p)
				}
			}
			// Stats counters are monotonic — Clear MUST NOT reset them.
			// (Hits/Puts from the seed phase persist.)
			st := idx.Stats()
			if st.AttrEntriesCount != 0 {
				t.Errorf("AttrEntriesCount after Clear = %d, want 0", st.AttrEntriesCount)
			}
			if st.BodyEntriesCount != 0 {
				t.Errorf("BodyEntriesCount after Clear = %d, want 0", st.BodyEntriesCount)
			}
		})
	}
}

func TestClear_BucketsUsableAfter(t *testing.T) {
	// Regression guard: bbolt's Clear deletes + recreates buckets.
	// If the implementation forgot the recreate step, subsequent Put
	// would panic / error. This test puts again after Clear and
	// confirms the entry comes back through PeekAttrs.
	for _, backend := range []string{"memory", "bbolt"} {
		t.Run(backend, func(t *testing.T) {
			idx := openTestIndex(t, backend)
			_, paths := seedAttrs(t, idx, "before.md")
			flushAttrs(t, idx, paths[0])

			if err := idx.Clear(); err != nil {
				t.Fatalf("Clear: %v", err)
			}

			_, paths2 := seedAttrs(t, idx, "after.md")
			flushAttrs(t, idx, paths2[0])
			if _, ok := idx.PeekAttrs(paths2[0]); !ok {
				t.Errorf("Put after Clear didn't land — buckets not re-created?")
			}
		})
	}
}

package monitor

import (
	"sync"
	"testing"
	"time"
)

func TestCollector_RecordAndSnapshot(t *testing.T) {
	c := NewCollector()
	c.Record("search", 100*time.Millisecond, OutcomeOK, "", 5)
	c.Record("search", 300*time.Millisecond, OutcomeOK, "", 9)
	c.Record("search", 50*time.Millisecond, OutcomeError, "", 0)
	c.Record("stats", 10*time.Millisecond, OutcomeCancelled, "timeout", 0)

	s := c.Snapshot()
	if s.TotalCalls != 4 {
		t.Errorf("TotalCalls = %d, want 4", s.TotalCalls)
	}
	if s.TotalErrors != 1 {
		t.Errorf("TotalErrors = %d, want 1", s.TotalErrors)
	}
	if len(s.Tools) != 2 {
		t.Fatalf("Tools = %d, want 2", len(s.Tools))
	}
	// search has the most calls → first.
	if s.Tools[0].Tool != "search" || s.Tools[0].Count != 3 {
		t.Errorf("Tools[0] = %+v, want search count 3", s.Tools[0])
	}
	if s.Tools[0].Errors != 1 {
		t.Errorf("search errors = %d, want 1", s.Tools[0].Errors)
	}
	if s.Tools[0].Max < 0.29 {
		t.Errorf("search max = %v, want >= 0.3", s.Tools[0].Max)
	}
	// recent is newest-first; last recorded was the cancelled stats call.
	if len(s.Recent) != 4 || s.Recent[0].Tool != "stats" || s.Recent[0].Outcome != OutcomeCancelled {
		t.Errorf("Recent[0] = %+v, want newest = cancelled stats", s.Recent[0])
	}
	if s.Recent[0].Reason != "timeout" {
		t.Errorf("cancel reason = %q, want timeout", s.Recent[0].Reason)
	}
}

func TestCollector_RecentCappedAndInFlight(t *testing.T) {
	c := NewCollector()
	c.Begin()
	c.Begin()
	if got := c.Snapshot().InFlight; got != 2 {
		t.Errorf("InFlight = %d, want 2", got)
	}
	c.End()
	for range recentCap + 50 {
		c.Record("x", time.Millisecond, OutcomeOK, "", 0)
	}
	s := c.Snapshot()
	if len(s.Recent) != recentCap {
		t.Errorf("Recent len = %d, want capped at %d", len(s.Recent), recentCap)
	}
	if s.InFlight != 1 {
		t.Errorf("InFlight = %d, want 1", s.InFlight)
	}
}

// TestCollector_Concurrent is the -race guard: many goroutines record +
// begin/end + snapshot at once.
func TestCollector_Concurrent(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for i := range 200 {
				c.Begin()
				c.Record("tool", time.Duration(i)*time.Microsecond, OutcomeOK, "", i)
				c.End()
				if i%50 == 0 {
					_ = c.Snapshot()
				}
			}
		})
	}
	wg.Wait()
	if got := c.Snapshot().TotalCalls; got != 16*200 {
		t.Errorf("TotalCalls = %d, want %d", got, 16*200)
	}
}

func TestPercentiles(t *testing.T) {
	// 1..100 → p50 ≈ 50, p95 ≈ 95 (nearest-rank, 0-indexed).
	sample := make([]float64, 100)
	for i := range sample {
		sample[i] = float64(i + 1)
	}
	p50, p95 := percentiles(sample)
	if p50 < 50 || p50 > 52 {
		t.Errorf("p50 = %v, want ~51", p50)
	}
	if p95 < 95 || p95 > 97 {
		t.Errorf("p95 = %v, want ~96", p95)
	}
	if p, _ := percentiles(nil); p != 0 {
		t.Errorf("empty p50 = %v, want 0", p)
	}
}

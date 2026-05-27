// Package monitor provides an opt-in, localhost-only HTTP dashboard for
// observing file-search-on's internal state while a long-running mode
// (the MCP server or the watch command) is active. It is read-only and
// adds no external dependencies — the UI is a single embedded page that
// polls a small JSON API.
//
// Collector lives here (rather than in mcpserver) so both the MCP
// handlers (which write tool-call telemetry) and the dashboard (which
// reads snapshots) can depend on it without an import cycle: mcpserver
// imports monitor; monitor never imports mcpserver.
package monitor

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// recentCap bounds the rolling recent-calls feed.
	recentCap = 256
	// latencyCap bounds the per-tool latency sample kept for percentiles.
	latencyCap = 128
)

// Outcome classifies how a tool call ended.
const (
	OutcomeOK        = "ok"
	OutcomeError     = "error"
	OutcomeCancelled = "cancelled"
)

// Collector records MCP tool-call telemetry. All methods are safe for
// concurrent use by many tool-handler goroutines.
type Collector struct {
	startedAt time.Time
	inflight  atomic.Int64

	mu          sync.Mutex
	perTool     map[string]*toolStat
	recent      []CallRecord // oldest → newest, capped at recentCap
	totalCalls  uint64
	totalErrors uint64
}

// toolStat accumulates per-tool counters plus a bounded sample of recent
// latencies (seconds) for percentile estimation.
type toolStat struct {
	count     uint64
	errors    uint64
	cancels   uint64
	maxSec    float64
	durations []float64 // bounded at latencyCap, drop-oldest
}

// CallRecord is one entry in the recent-calls feed.
type CallRecord struct {
	Tool    string    `json:"tool"`
	At      time.Time `json:"at"`
	Seconds float64   `json:"seconds"`
	Outcome string    `json:"outcome"`          // ok | error | cancelled
	Reason  string    `json:"reason,omitempty"` // cancellation reason when cancelled
	Count   int       `json:"count,omitempty"`  // result count when the tool reports one
}

// NewCollector returns a ready Collector with its uptime clock started.
func NewCollector() *Collector {
	return &Collector{
		startedAt: time.Now(),
		perTool:   make(map[string]*toolStat),
	}
}

// Begin marks a tool call as in-flight; pair with End (deferred).
func (c *Collector) Begin() { c.inflight.Add(1) }

// End marks an in-flight tool call as finished.
func (c *Collector) End() { c.inflight.Add(-1) }

// Record logs a completed tool call. outcome is one of the Outcome*
// constants; reason is the cancellation reason (empty otherwise); count
// is the tool's result count when known (0 otherwise).
func (c *Collector) Record(tool string, d time.Duration, outcome, reason string, count int) {
	sec := d.Seconds()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCalls++
	ts := c.perTool[tool]
	if ts == nil {
		ts = &toolStat{}
		c.perTool[tool] = ts
	}
	ts.count++
	switch outcome {
	case OutcomeError:
		ts.errors++
		c.totalErrors++
	case OutcomeCancelled:
		ts.cancels++
	}
	if sec > ts.maxSec {
		ts.maxSec = sec
	}
	if len(ts.durations) >= latencyCap {
		ts.durations = ts.durations[1:]
	}
	ts.durations = append(ts.durations, sec)

	if len(c.recent) >= recentCap {
		c.recent = c.recent[1:]
	}
	c.recent = append(c.recent, CallRecord{
		Tool: tool, At: time.Now(), Seconds: sec,
		Outcome: outcome, Reason: reason, Count: count,
	})
}

// Snapshot is an immutable view of the collector's state for the API.
type Snapshot struct {
	UptimeSeconds float64            `json:"uptime_seconds"`
	InFlight      int64              `json:"in_flight"`
	TotalCalls    uint64             `json:"total_calls"`
	TotalErrors   uint64             `json:"total_errors"`
	Tools         []ToolStatSnapshot `json:"tools"`
	Recent        []CallRecord       `json:"recent"` // newest first
}

// ToolStatSnapshot is the per-tool rollup shown in the activity panel.
type ToolStatSnapshot struct {
	Tool    string  `json:"tool"`
	Count   uint64  `json:"count"`
	Errors  uint64  `json:"errors"`
	Cancels uint64  `json:"cancels"`
	P50     float64 `json:"p50_seconds"`
	P95     float64 `json:"p95_seconds"`
	Max     float64 `json:"max_seconds"`
}

// Snapshot returns a copy of the current telemetry. Tools are sorted by
// call count descending; Recent is newest-first.
func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	tools := make([]ToolStatSnapshot, 0, len(c.perTool))
	for name, ts := range c.perTool {
		p50, p95 := percentiles(ts.durations)
		tools = append(tools, ToolStatSnapshot{
			Tool: name, Count: ts.count, Errors: ts.errors, Cancels: ts.cancels,
			P50: p50, P95: p95, Max: ts.maxSec,
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Count != tools[j].Count {
			return tools[i].Count > tools[j].Count
		}
		return tools[i].Tool < tools[j].Tool
	})

	recent := make([]CallRecord, len(c.recent))
	for i, r := range c.recent { // reverse → newest first
		recent[len(c.recent)-1-i] = r
	}

	return Snapshot{
		UptimeSeconds: time.Since(c.startedAt).Seconds(),
		InFlight:      c.inflight.Load(),
		TotalCalls:    c.totalCalls,
		TotalErrors:   c.totalErrors,
		Tools:         tools,
		Recent:        recent,
	}
}

// percentiles returns p50 and p95 of the sample (seconds). Copies and
// sorts so the caller's slice is untouched. Empty → (0, 0).
func percentiles(sample []float64) (p50, p95 float64) {
	if len(sample) == 0 {
		return 0, 0
	}
	s := make([]float64, len(sample))
	copy(s, sample)
	sort.Float64s(s)
	return s[pctIndex(len(s), 50)], s[pctIndex(len(s), 95)]
}

// pctIndex maps a percentile to a clamped index into a sorted slice of
// length n (nearest-rank).
func pctIndex(n, pct int) int {
	if n == 0 {
		return 0
	}
	idx := pct * n / 100
	if idx >= n {
		idx = n - 1
	}
	return idx
}

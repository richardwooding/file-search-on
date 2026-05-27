package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/monitor"
)

// callReporter is an optional interface a tool Output may implement so
// the activity collector can record its result count + cancellation
// status. Outputs that don't implement it record as ok/error only.
type callReporter interface {
	callReport() (count int, cancelled bool, reason string)
}

// instrument wraps a tool handler so each invocation is timed and
// recorded on the collector (in-flight gauge, per-tool latency, recent
// feed). When c is nil it returns h unchanged, so the no-monitor path —
// and every existing test that builds the server without a collector —
// pays nothing.
func instrument[In, Out any](
	c *monitor.Collector,
	name string,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	if c == nil {
		return h
	}
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		c.Begin()
		start := time.Now()
		res, out, err := h(ctx, req, in)
		c.End()

		outcome, reason, count := monitor.OutcomeOK, "", 0
		switch {
		case err != nil || (res != nil && res.IsError):
			outcome = monitor.OutcomeError
		default:
			if r, ok := any(out).(callReporter); ok {
				cnt, cancelled, rsn := r.callReport()
				count = cnt
				if cancelled {
					outcome = monitor.OutcomeCancelled
					reason = rsn
				}
			}
		}
		c.Record(name, time.Since(start), outcome, reason, count)
		return res, out, err
	}
}

// callReport implementations for the outputs that carry a result count
// and cancellation status. Value receivers so the wrapper's
// any(out).(callReporter) assertion succeeds on the returned value.

func (o SearchOutput) callReport() (int, bool, string) {
	return o.Count, o.Cancelled, o.CancellationReason
}

func (o SearchSemanticOutput) callReport() (int, bool, string) {
	return o.Count, o.Cancelled, o.CancellationReason
}

func (o FindMatchesOutput) callReport() (int, bool, string) {
	return o.Count, o.Cancelled, o.CancellationReason
}

func (o FindProjectsOutput) callReport() (int, bool, string) {
	return o.Count, o.Cancelled, o.CancellationReason
}

func (o DiffTreesOutput) callReport() (int, bool, string) {
	return o.Count, o.Cancelled, o.CancellationReason
}

func (o StatsOutput) callReport() (int, bool, string) {
	return int(o.TotalCount), o.Cancelled, o.CancellationReason
}

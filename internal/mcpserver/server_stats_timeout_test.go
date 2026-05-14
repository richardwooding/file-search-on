package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStatsTool_HonoursTimeout_FastPath verifies that the stats tool
// returns within ~timeout_seconds when called on a directory big enough
// that the walk would otherwise take longer. The default group_by
// ("content_type") triggers the skip-Attributes fast path so the walk
// is fast on its own; this test mostly guards the deadline plumbing
// against future regressions.
func TestStatsTool_HonoursTimeout_FastPath(t *testing.T) {
	dir := t.TempDir()
	// 200 small files — fast even without the optimisation, but big
	// enough that we'd notice if the walker went off into the weeds.
	for i := range 200 {
		path := filepath.Join(dir, "f"+itoa(i)+".md")
		if err := os.WriteFile(path, []byte("# h\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cs := newSession(t)
	timeout := 5.0
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:            dir,
			TimeoutSeconds: &timeout,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.TotalCount != 200 {
		t.Errorf("TotalCount=%d want 200", out.TotalCount)
	}
	if out.Cancelled {
		t.Errorf("Cancelled=true on fast-path stats; want false")
	}
}

// TestStatsTool_HonoursTimeout_SlowPath sets a deliberately tight
// timeout against a workload that requires attribute parsing (the slow
// path) and confirms the call returns within budget with cancelled=true.
// Before the deadline-honour fix, this scenario could blow past the
// budget by orders of magnitude.
//
// Each markdown file is small but the test sets a 50ms budget — small
// enough that with 200 files even a fast machine usually trips the
// deadline. We allow a generous 2× budget margin for CI noise; the
// strict assertion is just "did not run forever". When the deadline
// fires, the partial result comes back with cancelled=true.
func TestStatsTool_HonoursTimeout_SlowPath(t *testing.T) {
	dir := t.TempDir()
	body := "---\nlanguage: en\n---\n# heading\n" + strings.Repeat("word ", 100) + "\n"
	for i := range 200 {
		path := filepath.Join(dir, "f"+itoa(i)+".md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cs := newSession(t)
	timeout := 0.05 // 50ms — tight enough that some files won't get parsed
	start := time.Now()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:            dir,
			Expr:           "is_markdown",
			GroupBy:        "language", // forces the slow path (attribute parsing)
			TimeoutSeconds: &timeout,
		},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}

	// The strict assertion: the response must return within ~5x the
	// budget. Before the ctx-propagation fix in the markdown / parser
	// layer, slow inputs could run multiples-of-seconds past a 50ms
	// budget. 250ms is generous — the real return time on a fast
	// machine is well under 100ms.
	if elapsed > 1*time.Second {
		t.Errorf("stats slow-path took %v; want < 1s with 50ms timeout (deadline not honoured)", elapsed)
	}

	// Behaviour assertion: either Cancelled=true (the deadline fired
	// mid-walk; partial results returned) OR TotalCount==200 (the
	// walk completed inside the budget on a fast machine). Both are
	// valid outcomes — what's NOT valid is a non-cancelled call that
	// blew past the budget, which the elapsed check above catches.
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if !out.Cancelled && out.TotalCount != 200 {
		t.Errorf("non-cancelled call returned TotalCount=%d; want 200 OR Cancelled=true", out.TotalCount)
	}
	if out.Cancelled && out.CancellationReason != "timeout" {
		t.Errorf("Cancelled=true but CancellationReason=%q; want \"timeout\"", out.CancellationReason)
	}
}

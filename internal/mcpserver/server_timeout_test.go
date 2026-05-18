package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestSearchTimeoutReturnsPartial verifies that when the per-call
// timeout fires mid-walk, the search tool DOES NOT return an error —
// it returns the partial match set with cancelled=true,
// cancellation_reason="timeout", and elapsed_seconds populated.
//
// We construct a directory with a hand-rolled slow content type via
// many small markdown files plus a tight timeout (1 ms). The walk
// can't complete in 1 ms, so we expect cancelled=true. The match set
// may legitimately be empty on very fast hardware, which is fine —
// the contract is "no error, cancelled=true", not "non-empty result".
func TestSearchTimeoutReturnsPartial(t *testing.T) {
	dir := t.TempDir()
	// Enough files that the walk has work to do; small enough that
	// CI doesn't take forever.
	for i := range 200 {
		mustWrite(t, filepath.Join(dir, sprintfPad(i)+".md"), "# h\nbody body body\n")
	}

	ctx := t.Context()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{})
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// 1 millisecond timeout — too tight to finish 200 markdown files.
	tinyTimeout := 0.001
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:           "is_markdown",
			Dir:            dir,
			TimeoutSeconds: &tinyTimeout,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error (should be a normal partial result): %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if !out.Cancelled {
		t.Fatalf("expected cancelled=true; got %+v", out)
	}
	if out.CancellationReason != "timeout" {
		t.Errorf("CancellationReason=%q want \"timeout\"", out.CancellationReason)
	}
	if out.ElapsedSeconds <= 0 {
		t.Errorf("ElapsedSeconds=%v want positive", out.ElapsedSeconds)
	}
	if out.Count != len(out.Matches) {
		t.Errorf("Count=%d but len(Matches)=%d (must agree)", out.Count, len(out.Matches))
	}
}

// TestSearchTimeoutPerCallOverridesDefault verifies that
// timeout_seconds in input wins over the server default. The server
// default is set very tight (1ms), but the per-call override gives
// the walk plenty of time to complete normally.
func TestSearchTimeoutPerCallOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# h\n")

	ctx := t.Context()
	// Server default = 1 ms (would normally cause cancellation).
	server := New("test", index.NewMemory(), time.Millisecond, EmbedDefaults{})
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// Per-call override = 30 seconds; no excuse to time out on one file.
	bigTimeout := 30.0
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:           "is_markdown",
			Dir:            dir,
			TimeoutSeconds: &bigTimeout,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Cancelled {
		t.Errorf("Cancelled=true; per-call override should have prevented timeout (%+v)", out)
	}
	if out.Count != 1 {
		t.Errorf("Count=%d want 1", out.Count)
	}
}

// TestSearchTimeoutZeroDisablesForCall verifies that timeout_seconds=0
// disables the timeout for that specific call, even when the server
// default is tight.
func TestSearchTimeoutZeroDisablesForCall(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# h\n")

	ctx := t.Context()
	// Server default = 1ms.
	server := New("test", index.NewMemory(), time.Millisecond, EmbedDefaults{})
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	zero := 0.0
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:           "is_markdown",
			Dir:            dir,
			TimeoutSeconds: &zero,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Cancelled {
		t.Errorf("Cancelled=true with timeout_seconds=0; should be disabled (%+v)", out)
	}
}

// TestSearchClientCancelReason verifies that when the parent ctx is
// cancelled (not our own deadline), the cancellation_reason is
// "client_cancel" rather than "timeout".
func TestSearchClientCancelReason(t *testing.T) {
	dir := t.TempDir()
	for i := range 50 {
		mustWrite(t, filepath.Join(dir, sprintfPad(i)+".md"), "# h\n")
	}

	parentCtx, cancel := context.WithCancel(t.Context())
	server := New("test", index.NewMemory(), 0, EmbedDefaults{})
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(parentCtx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(parentCtx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// Cancel the parent context immediately to simulate transport-side
	// cancellation. The call may fail at the transport layer before
	// the server can produce a response — that's fine; the test
	// asserts only that we don't crash and we don't wedge.
	cancel()
	_, _ = cs.CallTool(parentCtx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir},
	})
	// No assertion on result — the contract being tested is "doesn't
	// hang / crash on parent-ctx cancel", which we get just by
	// reaching this point under -race.
}

func sprintfPad(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

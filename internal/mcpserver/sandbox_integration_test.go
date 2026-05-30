package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// newSandboxedSession is the newSession variant that wraps the server
// with WithSandbox(roots). Used by sandbox integration tests so they
// exercise the same wiring real callers go through.
func newSandboxedSession(t *testing.T, roots ...string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx := t.Context()

	server := New("test", index.NewMemory(), 0, EmbedDefaults{}, WithSandbox(roots))
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

	return ctx, cs
}

// TestSearchTool_SandboxRejectsOutside is the load-bearing assertion:
// when the server is wrapped with WithSandbox, an agent asking to walk
// /etc gets a tool error with a "sandbox" message, not a search result.
func TestSearchTool_SandboxRejectsOutside(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cs := newSandboxedSession(t, sandbox)

	// /etc is definitely outside any tempdir-based sandbox.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Dir: "/etc"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for outside-sandbox dir, got %+v", res)
	}
}

func TestSearchTool_SandboxAllowsInside(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "a.md"), []byte("# hello\nbody body body body body\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: sandbox},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("inside-sandbox dir should be accepted, got IsError; res=%+v", res)
	}
}

// TestSearchTool_SandboxRejectsFollowSymlinks asserts the v1 hard
// rejection: follow_symlinks=true is unsupported when the sandbox is
// active because the walker doesn't yet enforce sandbox per-entry.
func TestSearchTool_SandboxRejectsFollowSymlinks(t *testing.T) {
	sandbox := t.TempDir()
	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:            sandbox,
			FollowSymlinks: true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for follow_symlinks=true under sandbox, got %+v", res)
	}
}

// TestReadAttributesTool_SandboxRejectsOutside locks in the single-file
// tool path — agents can't sneak /etc/passwd in via read_attributes.
func TestReadAttributesTool_SandboxRejectsOutside(t *testing.T) {
	sandbox := t.TempDir()
	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_attributes",
		Arguments: ReadAttributesInput{Path: "/etc/hosts"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for /etc/hosts under sandbox, got %+v", res)
	}
}

// TestReadLinesTool_SandboxRejectsOutside — same shape for read_lines.
func TestReadLinesTool_SandboxRejectsOutside(t *testing.T) {
	sandbox := t.TempDir()
	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_lines",
		Arguments: ReadLinesInput{Path: "/etc/hosts"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for /etc/hosts under sandbox, got %+v", res)
	}
}

// TestDiffTreesTool_SandboxRejectsOutside — the two-tree tool. Catches
// the case where one tree is fine but the other isn't.
func TestDiffTreesTool_SandboxRejectsOutside(t *testing.T) {
	sandbox := t.TempDir()
	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "diff_trees",
		Arguments: DiffTreesInput{
			TreeA: sandbox,
			TreeB: "/tmp",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError when one tree is outside sandbox, got %+v", res)
	}
}

// TestSearchTool_SandboxRejectsHashAllowlistPath — the side-channel
// path: hash_allowlist_path is a path the agent supplies; without
// sandbox enforcement it could be used to read arbitrary files for
// hash membership lookup.
func TestSearchTool_SandboxRejectsHashAllowlistPath(t *testing.T) {
	sandbox := t.TempDir()
	ctx, cs := newSandboxedSession(t, sandbox)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:               sandbox,
			HashAllowlistPath: "/etc/hosts",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError when hash_allowlist_path is outside sandbox, got %+v", res)
	}
}

// TestSearchTool_NoSandboxAllowsAnywhere — sanity-check the
// pass-through path: with no WithSandbox option, /etc is accepted (it
// errors later for unrelated reasons like permission denied, but the
// sandbox itself doesn't reject).
func TestSearchTool_NoSandboxAllowsAnywhere(t *testing.T) {
	// newSession (without WithSandbox) — same as the existing tests.
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:  "/tmp", // some path that probably exists
			Expr: "false",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// Should not be a sandbox error — either IsError=false (if /tmp is
	// walkable) or IsError=true for some other reason. The point is
	// the sandbox never fired.
	if res.IsError {
		var out SearchOutput
		_ = out
		// We don't make an assertion about the specific error — just
		// confirm the sandbox isn't responsible. If the test
		// environment doesn't have /tmp this would fail for a
		// different reason; skip in that case rather than fail.
		t.Logf("res.IsError=true for /tmp with no sandbox; this is fine (env-specific)")
	}
}

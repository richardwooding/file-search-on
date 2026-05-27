package mcpserver

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// newSessionWithVersion is newSession's twin that takes an explicit
// version stamp so server_version regression tests can pin a known
// value rather than the default "test".
func newSessionWithVersion(t *testing.T, version string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx := t.Context()

	server := New(version, index.NewMemory(), 0, EmbedDefaults{})
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

// TestServerVersionInResponses exercises every MCP tool that doesn't
// need external infrastructure and asserts CommonOutput.ServerVersion
// lands on the response. The point isn't to test tool behaviour
// (other tests do that) — it's to guard the wiring so adding a new
// tool without populating ServerVersion fails CI.
//
// search_semantic is excluded because it requires Ollama; its
// success-path wiring is identical to the other tools and exercised
// by manual smoke.
func TestServerVersionInResponses(t *testing.T) {
	const stamp = "test-v9.9.9"

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# Heading\n\nbody line 1\nbody line 2\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package main\n\nfunc main() {}\n")
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/x\n\ngo 1.26\n")
	// Two identical files so find_duplicates has a real result.
	mustWrite(t, filepath.Join(dir, "dup1.txt"), "same content here\n")
	mustWrite(t, filepath.Join(dir, "dup2.txt"), "same content here\n")

	// Build a minimal zip archive so list_archive_contents /
	// read_file_in_archive have a working fixture.
	archivePath := filepath.Join(dir, "fixture.zip")
	mustWriteZip(t, archivePath, map[string]string{
		"inside.txt": "hello from inside archive\n",
	})

	ctx, cs := newSessionWithVersion(t, stamp)

	type call struct {
		name string
		args any
		// decode receives the StructuredContent and returns the
		// embedded ServerVersion via the CommonOutput field.
		decode func(t *testing.T, res *mcp.CallToolResult) string
	}

	calls := []call{
		{
			name: "search",
			args: SearchInput{Expr: "is_markdown", Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o SearchOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "stats",
			args: StatsInput{Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o StatsOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "find_matches",
			args: FindMatchesInput{Pattern: "body", Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o FindMatchesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "find_duplicates",
			args: FindDuplicatesInput{Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o FindDuplicatesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "find_near_duplicates",
			args: FindNearDuplicatesInput{Dir: dir, Threshold: 0.85},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o FindNearDuplicatesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "read_lines",
			args: ReadLinesInput{Path: filepath.Join(dir, "a.md")},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ReadLinesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "read_attributes",
			args: ReadAttributesInput{Path: filepath.Join(dir, "a.md")},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ReadAttributesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "list_attributes",
			args: struct{}{},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ListAttributesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "index_stats",
			args: struct{}{},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o IndexStatsOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "list_archive_contents",
			args: ListArchiveContentsInput{Path: archivePath},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ListArchiveContentsOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "read_file_in_archive",
			args: ReadFileInArchiveInput{ArchivePath: archivePath, EntryPath: "inside.txt"},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ReadFileInArchiveOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "detect_project",
			args: DetectProjectInput{Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o DetectProjectOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "find_projects",
			args: FindProjectsInput{Dir: dir},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o FindProjectsOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "resolve_project_for_path",
			args: ResolveProjectForPathInput{Path: filepath.Join(dir, "b.go")},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ResolveProjectForPathOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			// No controller attached in this test server, so monitor_info
			// reports unavailable — but still stamps ServerVersion.
			name: "monitor_info",
			args: MonitorInfoInput{},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o MonitorInfoOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "list_presets",
			args: ListPresetsInput{},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o ListPresetsOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			// Short fixed window so the bounded watch returns promptly;
			// we only assert the version wiring, not match behaviour.
			name: "watch_search",
			args: WatchSearchInput{Dir: dir, DurationSeconds: 0.2},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o WatchSearchOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "diff_trees",
			args: DiffTreesInput{TreeA: dir, TreeB: dir, Op: "a-minus-b"},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o DiffTreesOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
		{
			name: "query_preset",
			args: QueryPresetInput{Name: "recent_changes", Dir: dir, Limit: 1},
			decode: func(t *testing.T, res *mcp.CallToolResult) string {
				var o SearchOutput
				mustDecodeStructured(t, res, &o)
				return o.ServerVersion
			},
		},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{
				Name:      c.name,
				Arguments: c.args,
			})
			if err != nil {
				t.Fatalf("CallTool %s: %v", c.name, err)
			}
			if res.GetError() != nil {
				t.Fatalf("tool %s returned error: %v", c.name, res.GetError())
			}
			got := c.decode(t, res)
			if got != stamp {
				t.Fatalf("%s: ServerVersion = %q, want %q", c.name, got, stamp)
			}
		})
	}
}

func mustWriteZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip %s: %v", path, err)
	}
}

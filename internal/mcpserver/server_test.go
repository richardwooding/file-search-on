package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newSession(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx := t.Context()

	server := New("test")
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

func TestSearchTool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# hello\n\nbody body body body body\n")
	mustWrite(t, filepath.Join(dir, "b.json"), `{"x":1}`)
	mustWrite(t, filepath.Join(dir, "c.txt"), "plain text, no content type registered\n")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr: "is_markdown",
			Dir:  dir,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 1 {
		t.Fatalf("expected 1 markdown match, got %d (%+v)", out.Count, out.Matches)
	}
	if got := filepath.Base(out.Matches[0].Path); got != "a.md" {
		t.Fatalf("expected a.md, got %s", got)
	}
	if out.Matches[0].ContentType != "markdown" {
		t.Fatalf("expected content_type=markdown, got %s", out.Matches[0].ContentType)
	}
}

func TestSearchToolEmptyExprMatchesAll(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "x")
	mustWrite(t, filepath.Join(dir, "b.json"), `{}`)

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 2 {
		t.Fatalf("expected 2 matches with empty expr, got %d", out.Count)
	}
}

func TestSearchToolReturnsAttributes(t *testing.T) {
	dir := t.TempDir()
	body := "---\ntitle: Hello\nauthor: Jane\nlanguage: en\n---\n# h1\n\nbody body body\n"
	mustWrite(t, filepath.Join(dir, "post.md"), body)

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("expected 1 match, got %d", out.Count)
	}
	m := out.Matches[0]
	if m.Title != "Hello" {
		t.Errorf("title = %q, want Hello", m.Title)
	}
	if m.Author != "Jane" {
		t.Errorf("author = %q, want Jane", m.Author)
	}
	if m.Language != "en" {
		t.Errorf("language = %q, want en", m.Language)
	}
	if m.WordCount == 0 {
		t.Errorf("word_count = 0, want non-zero")
	}
	if !m.IsMarkdown {
		t.Errorf("is_markdown = false, want true")
	}
	if m.FrontmatterFormat != "yaml" {
		t.Errorf("frontmatter_format = %q, want yaml", m.FrontmatterFormat)
	}
}

func TestReadAttributesTool(t *testing.T) {
	dir := t.TempDir()
	body := "---\ntitle: Solo\nauthor: K\nlanguage: en\ntags:\n  - solo\n---\n# h1\n\nbody body body body\n"
	path := filepath.Join(dir, "post.md")
	mustWrite(t, path, body)

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_attributes",
		Arguments: ReadAttributesInput{Path: path},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var m SearchMatch
	mustDecodeStructured(t, res, &m)

	if m.Path != path {
		t.Errorf("path = %q, want %q", m.Path, path)
	}
	if m.ContentType != "markdown" {
		t.Errorf("content_type = %q, want markdown", m.ContentType)
	}
	if m.Title != "Solo" {
		t.Errorf("title = %q, want Solo", m.Title)
	}
	if m.Author != "K" {
		t.Errorf("author = %q, want K", m.Author)
	}
	if !m.IsMarkdown {
		t.Errorf("is_markdown = false, want true")
	}
	if m.FrontmatterFormat != "yaml" {
		t.Errorf("frontmatter_format = %q, want yaml", m.FrontmatterFormat)
	}
	if len(m.Tags) != 1 || m.Tags[0] != "solo" {
		t.Errorf("tags = %v, want [solo]", m.Tags)
	}
}

func TestReadAttributesToolMissingPath(t *testing.T) {
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_attributes",
		Arguments: ReadAttributesInput{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for empty path; got false")
	}
}

func TestListAttributesTool(t *testing.T) {
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: struct{}{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out ListAttributesOutput
	mustDecodeStructured(t, res, &out)

	if len(out.Schema.Common) == 0 {
		t.Fatal("expected non-empty Common schema")
	}
	if len(out.ContentTypes) == 0 {
		t.Fatal("expected at least one registered content type")
	}

	// Sanity: at least one expected attribute is present.
	found := false
	for _, a := range out.Schema.Common {
		if a.Name == "is_markdown" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected is_markdown in Common attributes")
	}
}

// TestSearchTool_ProgressNotifications verifies that when a client
// passes a progressToken on a search call, the server emits at least
// one progress notification mid-walk (stride is 50; we create 120
// matches to guarantee at least 2 notifications fire).
func TestSearchTool_ProgressNotifications(t *testing.T) {
	dir := t.TempDir()
	for i := range 120 {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("doc-%03d.md", i)), "# h\n")
	}

	ctx := t.Context()
	server := New("test")
	t1, t2 := mcp.NewInMemoryTransports()

	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	var notified atomic.Int64
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			if req.Params.ProgressToken == "search-progress-test" {
				notified.Add(1)
			}
		},
	})
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	params := &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir},
	}
	params.SetProgressToken("search-progress-test")

	res, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 120 {
		t.Fatalf("expected 120 matches, got %d", out.Count)
	}
	// 120 matches / 50 stride = 2 notifications minimum (at 50, 100).
	if got := notified.Load(); got < 2 {
		t.Errorf("expected >= 2 progress notifications, got %d", got)
	}
}

// TestSearchTool_NoProgressTokenStaysSilent verifies that without a
// progress token, the server emits no progress notifications even on
// large result sets.
func TestSearchTool_NoProgressTokenStaysSilent(t *testing.T) {
	dir := t.TempDir()
	for i := range 120 {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("doc-%03d.md", i)), "# h\n")
	}

	ctx := t.Context()
	server := New("test")
	t1, t2 := mcp.NewInMemoryTransports()

	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	var notified atomic.Int64
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, _ *mcp.ProgressNotificationClientRequest) {
			notified.Add(1)
		},
	})
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 120 {
		t.Fatalf("expected 120 matches, got %d", out.Count)
	}
	if got := notified.Load(); got != 0 {
		t.Errorf("expected 0 progress notifications without token, got %d", got)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustDecodeStructured(t *testing.T, res *mcp.CallToolResult, into any) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatal("expected StructuredContent on tool result")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-marshal structured content: %v", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode structured content: %v\nraw: %s", err, raw)
	}
}

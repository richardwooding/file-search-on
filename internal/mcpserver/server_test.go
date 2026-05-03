package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

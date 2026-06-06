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

	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

func newSession(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
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

	var m search.Match
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

func TestListAttributesTool_SummaryByDefault(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_attributes", Arguments: ListAttributesInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ListAttributesOutput
	mustDecodeStructured(t, res, &out)

	if out.Mode != "summary" {
		t.Fatalf("Mode = %q, want summary", out.Mode)
	}
	if out.Summary == nil {
		t.Fatal("Summary nil in summary mode")
	}
	for _, sec := range []string{"common", "type_specific", "frontmatter", "functions", "content_types"} {
		if n, ok := out.Summary.Sections[sec]; !ok || n == 0 {
			t.Errorf("Summary.Sections[%q] = %d/%v, want >0", sec, n, ok)
		}
	}
	if len(out.Summary.FunctionNames) == 0 {
		t.Error("Summary.FunctionNames empty; want >=1")
	}
	if out.Summary.Hint == "" {
		t.Error("Summary.Hint empty; want drill-in instructions")
	}
	// Detail fields must be empty in summary mode.
	if len(out.Attributes) != 0 || len(out.Functions) != 0 || len(out.ContentTypes) != 0 {
		t.Error("detail fields should be empty in summary mode")
	}
}

func TestListAttributesTool_SummaryStaysSmall(t *testing.T) {
	// The whole point of #273: the summary mode must be token-budget-safe.
	// The legacy full-schema response is ~100kB. Assert the summary is
	// dramatically smaller — under 4kB even with the hint string and
	// function-name list inflating it.
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_attributes", Arguments: ListAttributesInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if sc := res.StructuredContent; sc != nil {
		raw, err := json.Marshal(sc)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if len(raw) > 4096 {
			t.Errorf("summary response = %d bytes, want < 4096 (#273 budget)", len(raw))
		}
	}
}

func TestListAttributesTool_SectionCommon(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: ListAttributesInput{Section: "common"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ListAttributesOutput
	mustDecodeStructured(t, res, &out)

	if out.Mode != "section" || out.Section != "common" {
		t.Errorf("Mode=%q Section=%q, want section/common", out.Mode, out.Section)
	}
	if out.Total == 0 {
		t.Fatal("Total = 0; want >0 common attrs")
	}
	if len(out.Attributes) == 0 {
		t.Fatal("Attributes empty for section=common")
	}
	// is_markdown is a known Common entry — guard the regression.
	found := false
	for _, a := range out.Attributes {
		if a.Name == "is_markdown" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected is_markdown in section=common slice")
	}
}

func TestListAttributesTool_SectionPagination(t *testing.T) {
	ctx, cs := newSession(t)
	// First page: limit 5, offset 0.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: ListAttributesInput{Section: "type_specific", Limit: 5, Offset: 0},
	})
	if err != nil {
		t.Fatalf("page1 CallTool: %v", err)
	}
	var page1 ListAttributesOutput
	mustDecodeStructured(t, res, &page1)
	if len(page1.Attributes) != 5 {
		t.Errorf("page1 size = %d, want 5", len(page1.Attributes))
	}

	// Second page: limit 5, offset 5 — must be distinct entries.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: ListAttributesInput{Section: "type_specific", Limit: 5, Offset: 5},
	})
	if err != nil {
		t.Fatalf("page2 CallTool: %v", err)
	}
	var page2 ListAttributesOutput
	mustDecodeStructured(t, res, &page2)
	if len(page2.Attributes) != 5 {
		t.Errorf("page2 size = %d, want 5", len(page2.Attributes))
	}
	if page1.Attributes[0].Name == page2.Attributes[0].Name {
		t.Errorf("page1 and page2 first entries identical (%q); pagination not advancing", page1.Attributes[0].Name)
	}
	// Same total reported on both pages.
	if page1.Total != page2.Total {
		t.Errorf("Total differs between pages: %d vs %d", page1.Total, page2.Total)
	}
}

func TestListAttributesTool_SectionFunctions(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: ListAttributesInput{Section: "functions"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ListAttributesOutput
	mustDecodeStructured(t, res, &out)
	if out.Mode != "section" || out.Section != "functions" {
		t.Errorf("Mode=%q Section=%q", out.Mode, out.Section)
	}
	if len(out.Functions) == 0 {
		t.Fatal("Functions empty for section=functions")
	}
	if len(out.Attributes) != 0 {
		t.Errorf("Attributes should be empty for section=functions; got %d", len(out.Attributes))
	}
}

func TestListAttributesTool_NamesAcrossSections(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_attributes",
		Arguments: ListAttributesInput{
			// Mix of attribute (loc — type_specific) + function (levenshtein) + content_type (markdown).
			Names: []string{"loc", "levenshtein", "markdown"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ListAttributesOutput
	mustDecodeStructured(t, res, &out)
	if out.Mode != "names" {
		t.Errorf("Mode = %q, want names", out.Mode)
	}

	gotAttrs := make(map[string]bool, len(out.Attributes))
	for _, a := range out.Attributes {
		gotAttrs[a.Name] = true
	}
	if !gotAttrs["loc"] {
		t.Error("expected 'loc' in Attributes")
	}

	gotFuncs := make(map[string]bool, len(out.Functions))
	for _, f := range out.Functions {
		gotFuncs[f.Name] = true
	}
	if !gotFuncs["levenshtein"] {
		t.Error("expected 'levenshtein' in Functions")
	}

	gotCTs := make(map[string]bool, len(out.ContentTypes))
	for _, c := range out.ContentTypes {
		gotCTs[c.Name] = true
	}
	if !gotCTs["markdown"] {
		t.Error("expected 'markdown' in ContentTypes")
	}
}

func TestListAttributesTool_UnknownSectionErrors(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_attributes",
		Arguments: ListAttributesInput{Section: "bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// MCP wraps handler errors into the CallToolResult via IsError=true.
	if !res.IsError {
		t.Errorf("expected IsError=true for unknown section; got %+v", res)
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
	server := New("test", index.NewMemory(), 0, EmbedDefaults{})
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
	server := New("test", index.NewMemory(), 0, EmbedDefaults{})
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

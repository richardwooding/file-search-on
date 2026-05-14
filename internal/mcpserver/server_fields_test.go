package mcpserver

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/search"
)

// TestSearchTool_FieldsProjection verifies that passing 'fields'
// strips non-listed attributes from each match while keeping the
// always-on triplet (path / content_type / size).
func TestSearchTool_FieldsProjection(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "---\ntitle: Hello\nauthor: Jane\n---\n# h\nbody body body\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:    dir,
			Expr:   "is_markdown",
			Fields: []string{"title"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}

	// Decode via raw JSON so we can assert which keys appear, not just
	// what the typed SearchOutput shape carries.
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}
	if len(doc.Matches) != 1 {
		t.Fatalf("len(matches)=%d want 1; raw=%s", len(doc.Matches), raw)
	}
	m := doc.Matches[0]
	// Always-on:
	for _, want := range []string{"path", "content_type", "size"} {
		if _, ok := m[want]; !ok {
			t.Errorf("missing always-on field %q in %+v", want, m)
		}
	}
	// Requested:
	if got, _ := m["title"].(string); got != "Hello" {
		t.Errorf("title=%q want Hello (raw=%s)", got, raw)
	}
	// Should NOT appear (omitempty + zeroed):
	for _, banned := range []string{"author", "word_count", "is_markdown", "frontmatter", "tags"} {
		if _, present := m[banned]; present {
			t.Errorf("field %q should be projected away; got map=%+v", banned, m)
		}
	}
}

// TestSearchTool_FieldsEmptyReturnsEverything verifies that omitting
// 'fields' keeps the existing wire shape (all populated attributes).
func TestSearchTool_FieldsEmptyReturnsEverything(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "---\ntitle: Hello\nauthor: Jane\n---\n# h\nbody body body\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:  dir,
			Expr: "is_markdown",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out struct {
		Matches []search.Match `json:"matches"`
	}
	mustDecodeStructured(t, res, &out)
	if len(out.Matches) != 1 {
		t.Fatalf("len(matches)=%d want 1", len(out.Matches))
	}
	m := out.Matches[0]
	if m.Title != "Hello" {
		t.Errorf("Title=%q want Hello", m.Title)
	}
	if m.Author != "Jane" {
		t.Errorf("Author=%q want Jane (no projection should drop it)", m.Author)
	}
	if !m.IsMarkdown {
		t.Errorf("IsMarkdown=false want true (no projection)")
	}
}

// TestSearchTool_FieldsUnknownErrors verifies that a typo in 'fields'
// surfaces as a tool error before the walk runs.
func TestSearchTool_FieldsUnknownErrors(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# h\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:    dir,
			Fields: []string{"not_a_real_field"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for unknown field; got false")
	}
	// Error text should name the offending key. The SDK puts the
	// handler's error string in res.Content as a TextContent entry.
	var errText strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			errText.WriteString(tc.Text)
		}
	}
	if !strings.Contains(errText.String(), "not_a_real_field") {
		t.Errorf("error %q lacks the offending field name", errText.String())
	}
}

// TestSearchTool_FieldsSortStillWorks verifies that sort_by uses
// attributes regardless of whether they're in the response — sort
// happens before projection.
func TestSearchTool_FieldsSortStillWorks(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "small.md"), "# h\nshort body\n")
	mustWrite(t, filepath.Join(dir, "big.md"), "# h\n"+strings.Repeat("word ", 200)+"\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dir:    dir,
			Expr:   "is_markdown",
			SortBy: "word_count",
			Order:  "desc",
			Fields: []string{"title"}, // word_count NOT in fields
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Matches) != 2 {
		t.Fatalf("got %d matches", len(doc.Matches))
	}
	// big.md (more words) should come first because sort_by=word_count desc.
	firstPath, _ := doc.Matches[0]["path"].(string)
	if !strings.HasSuffix(firstPath, "big.md") {
		t.Errorf("first match=%q want big.md (sort by word_count desc)", firstPath)
	}
	// word_count itself should NOT appear in the response.
	if _, present := doc.Matches[0]["word_count"]; present {
		t.Errorf("word_count leaked into projection: %+v", doc.Matches[0])
	}
}

// TestReadAttributesTool_FieldsProjection mirrors the search test for
// the single-path tool — same projection vocabulary, same always-on
// triplet.
func TestReadAttributesTool_FieldsProjection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	mustWrite(t, p, "---\ntitle: Hello\nauthor: Jane\n---\n# h\nbody\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "read_attributes",
		Arguments: ReadAttributesInput{
			Path:   p,
			Fields: []string{"title"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}
	for _, want := range []string{"path", "content_type", "size", "title"} {
		if _, ok := m[want]; !ok {
			t.Errorf("missing %q in %+v", want, m)
		}
	}
	for _, banned := range []string{"author", "is_markdown"} {
		if _, present := m[banned]; present {
			t.Errorf("field %q should be projected away; got %+v", banned, m)
		}
	}
}

// TestReadAttributesTool_FieldsUnknownErrors verifies symmetry with
// search's validation path.
func TestReadAttributesTool_FieldsUnknownErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	mustWrite(t, p, "# h\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "read_attributes",
		Arguments: ReadAttributesInput{
			Path:   p,
			Fields: []string{"bogus_field"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for unknown field")
	}
}

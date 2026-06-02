package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSearchTool_IncludeBody_ContainsFilter verifies the body
// variable is reachable from the CEL expression and acts as a real
// content filter — files whose body doesn't match get pruned.
func TestSearchTool_IncludeBody_ContainsFilter(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "hit.md"), "# h\ntransformer attention is all you need\n")
	mustWrite(t, filepath.Join(dir, "miss.md"), "# m\nsomething about cabbage\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:        `is_markdown && body.contains("transformer")`,
			Dir:         dir,
			IncludeBody: true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1", out.Count)
	}
	if !strings.HasSuffix(out.Matches[0].Path, "hit.md") {
		t.Errorf("got %s, want hit.md", out.Matches[0].Path)
	}
}

// TestSearchTool_BodyMatches_Regex verifies CEL's built-in `matches`
// operator (RE2 regex) works against body — no custom function
// needed, as documented in server instructions.
func TestSearchTool_BodyMatches_Regex(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "todo.md"), "# h\n// TODO: fix\n")
	mustWrite(t, filepath.Join(dir, "done.md"), "# h\n// all done\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:        `is_markdown && body.matches("(?i)\\bTODO\\b")`,
			Dir:         dir,
			IncludeBody: true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1", out.Count)
	}
	if !strings.HasSuffix(out.Matches[0].Path, "todo.md") {
		t.Errorf("got %s, want todo.md", out.Matches[0].Path)
	}
}

// TestStatsTool_Histogram is the headline test for the new stats
// tool: walks a small mixed tree and asserts the returned
// histogram + totals.
func TestStatsTool_Histogram(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWrite(t, filepath.Join(dir, "b.md"), "# b\n")
	mustWrite(t, filepath.Join(dir, "x.json"), `{"k":1}`)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "stats",
		Arguments: StatsInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.TotalCount != 3 {
		t.Errorf("TotalCount=%d want 3", out.TotalCount)
	}
	byName := map[string]int64{}
	for _, b := range out.ContentTypes {
		byName[b.Name] = b.Count
	}
	if byName["markdown"] != 2 {
		t.Errorf("markdown count=%d want 2", byName["markdown"])
	}
	if byName["json"] != 1 {
		t.Errorf("json count=%d want 1", byName["json"])
	}
}

// TestStatsTool_ScopedByExpr verifies the optional expr parameter
// scopes the histogram.
func TestStatsTool_ScopedByExpr(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWrite(t, filepath.Join(dir, "x.json"), `{"k":1}`)
	mustWrite(t, filepath.Join(dir, "y.json"), `{"k":2}`)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "stats",
		Arguments: StatsInput{Dir: dir, Expr: "is_json"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.TotalCount != 2 {
		t.Errorf("TotalCount=%d want 2 (json only)", out.TotalCount)
	}
	if len(out.ContentTypes) != 1 || out.ContentTypes[0].Name != "json" {
		t.Errorf("ContentTypes=%v want just [json]", out.ContentTypes)
	}
}

// TestServerInstructionsMentionsBodyAndStats asserts the
// discoverability promise: agents that read InitializeResult.
// Instructions on connect should see `body`, `matches`, `contains`,
// and the `stats` tool mentioned without having to call
// list_attributes.
func TestServerInstructionsMentionsBodyAndStats(t *testing.T) {
	ctx, cs := newSession(t)
	_ = ctx
	init := cs.InitializeResult()
	if init == nil {
		t.Fatal("no InitializeResult")
	}
	for _, want := range []string{"body", "body.contains", "body.matches", "stats"} {
		if !strings.Contains(init.Instructions, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
}

// TestSearchTool_BodyContains_SuggestsFindMatches confirms the #281
// always-on hint lands in Suggestions[] when the search expression
// uses body.contains() — even on a successful (non-cancelled) walk.
// Agents discovering search first then learn to drop to find_matches
// for per-line context.
func TestSearchTool_BodyContains_SuggestsFindMatches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# h\ntransformer is here\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:        `is_markdown && body.contains("transformer")`,
			Dir:         dir,
			IncludeBody: true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Cancelled {
		t.Fatalf("walk should have completed cleanly; got Cancelled=true")
	}
	if len(out.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion for body.contains-based expr")
	}
	gotHint := false
	for _, s := range out.Suggestions {
		if strings.Contains(s, "find_matches") && strings.Contains(s, "transformer") {
			gotHint = true
			break
		}
	}
	if !gotHint {
		t.Errorf("suggestions missing find_matches hint with extracted pattern; got %v", out.Suggestions)
	}
}

// TestSearchTool_NoBodyMethod_NoHint confirms the hint stays out of
// the response on a plain-attribute search.
func TestSearchTool_NoBodyMethod_NoHint(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# h\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr: `is_markdown`,
			Dir:  dir,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	for _, s := range out.Suggestions {
		if strings.Contains(s, "find_matches") {
			t.Errorf("unexpected find_matches hint on non-body expr: %q", s)
		}
	}
}

package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// annSession spins up a server whose mock Ollama returns the same vector
// for every embed (query + files), so every text file clears the
// threshold — letting the tests focus on the ANN warm/cold/stale
// transitions rather than ranking.
func annSession(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	t.Cleanup(ollama.Close)

	ctx := t.Context()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{Server: ollama.URL, Model: "mock"})
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

func semCall(t *testing.T, ctx context.Context, cs *mcp.ClientSession, dir string) SearchSemanticOutput {
	t.Helper()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_semantic",
		Arguments: SearchSemanticInput{Query: "anything", Dir: dir, Threshold: 0.4, Limit: 50},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out SearchSemanticOutput
	mustDecodeStructured(t, res, &out)
	return out
}

// TestSearchSemantic_AnnWarmThenFast: the first call is the cold full
// walk (ann_used=false) and warms the index; the second identical call
// is served from the warm vector index (ann_used=true) with the same
// matches.
func TestSearchSemantic_AnnWarmThenFast(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n\nalpha content here\n")
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n\nbeta content here\n")

	ctx, cs := annSession(t)

	cold := semCall(t, ctx, cs, dir)
	if cold.AnnUsed {
		t.Error("first call should be the cold full walk (ann_used=false)")
	}
	if cold.Count != 2 {
		t.Fatalf("cold count = %d, want 2", cold.Count)
	}

	warm := semCall(t, ctx, cs, dir)
	if !warm.AnnUsed {
		t.Error("second call should use the warm ANN index (ann_used=true)")
	}
	if warm.Count != cold.Count {
		t.Errorf("warm count = %d, want %d (same as cold)", warm.Count, cold.Count)
	}
}

// TestSearchSemantic_AnnInvalidatedByNewFile: adding a file changes the
// directory fingerprint, so the next call falls back to a full walk
// (ann_used=false) and picks up the new file, then re-warms.
func TestSearchSemantic_AnnInvalidatedByNewFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n\nalpha\n")

	ctx, cs := annSession(t)

	_ = semCall(t, ctx, cs, dir)        // cold, warms
	warm := semCall(t, ctx, cs, dir)    // warm
	if !warm.AnnUsed {
		t.Fatalf("expected warm fast path before the new file")
	}

	// Add a file — bumps the dir mtime → fingerprint changes.
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n\nbeta\n")

	after := semCall(t, ctx, cs, dir)
	if after.AnnUsed {
		t.Error("adding a file must invalidate coverage → full walk (ann_used=false)")
	}
	if after.Count != 2 {
		t.Errorf("after add: count = %d, want 2 (new file picked up)", after.Count)
	}

	// And it re-warms for the next call.
	rewarm := semCall(t, ctx, cs, dir)
	if !rewarm.AnnUsed || rewarm.Count != 2 {
		t.Errorf("expected re-warm: ann_used=%v count=%d (want true/2)", rewarm.AnnUsed, rewarm.Count)
	}
}

// TestSearchSemantic_AnnStaleSkipOnContentEdit: editing a file's content
// doesn't change the dir structure, so the call still takes the warm
// path — but the edited file's cached vector is stale (size/mtime
// changed), so it's dropped and counted in ann_stale_skipped.
func TestSearchSemantic_AnnStaleSkipOnContentEdit(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n\nalpha\n")
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n\nbeta\n")

	ctx, cs := annSession(t)
	_ = semCall(t, ctx, cs, dir)     // cold, warms
	warm := semCall(t, ctx, cs, dir) // warm
	if !warm.AnnUsed || warm.Count != 2 {
		t.Fatalf("expected warm path with 2 matches; got ann_used=%v count=%d", warm.AnnUsed, warm.Count)
	}

	// Rewrite a.md with materially different (longer) content → size +
	// mtime change → its cached vector is stale.
	mustWrite(t, filepath.Join(dir, "a.md"), "# A\n\nalpha content rewritten substantially longer now\n")

	edited := semCall(t, ctx, cs, dir)
	if !edited.AnnUsed {
		t.Error("a content edit shouldn't change the dir structure → still warm path")
	}
	if edited.AnnStaleSkipped < 1 {
		t.Errorf("edited file's stale vector should be skipped; ann_stale_skipped=%d want >=1", edited.AnnStaleSkipped)
	}
	if edited.Count != 1 {
		t.Errorf("stale file dropped → count = %d, want 1 (only b.md)", edited.Count)
	}
}

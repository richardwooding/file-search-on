package mcpserver

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestServerInstructionsExposesPredicates is the proactive-discovery
// test: when a client connects to the server, the InitializeResult's
// Instructions field must mention every is_* type predicate inline so
// agents discover the vocabulary without an extra list_attributes
// round-trip. Failing this test means a content family was added (or
// renamed) without updating serverInstructions.
func TestServerInstructionsExposesPredicates(t *testing.T) {
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

	init := cs.InitializeResult()
	if init == nil {
		t.Fatal("client received no InitializeResult")
		return // unreachable; quiets staticcheck SA5011 (see note below)
	}
	got := init.Instructions
	if got == "" {
		t.Fatal("server instructions are empty; clients won't get proactive vocabulary")
	}

	predicates := []string{
		"is_markdown", "is_pdf", "is_html", "is_xml", "is_json", "is_csv",
		"is_text", "is_image", "is_audio", "is_video", "is_office", "is_epub",
		"is_archive", "is_binary", "is_email", "is_source",
	}
	for _, p := range predicates {
		if !strings.Contains(got, p) {
			t.Errorf("server instructions missing predicate %q", p)
		}
	}

	// Spot-check a few attribute names and the four tools.
	for _, k := range []string{
		"word_count", "page_count", "iso", "sample_rate", "video_height",
		"search", "list_attributes", "read_attributes", "index_stats",
	} {
		if !strings.Contains(got, k) {
			t.Errorf("server instructions missing %q", k)
		}
	}
}

// TestSearchToolDescriptionMentionsPredicates is the same idea but for
// clients that surface tool descriptions (not server-level instructions)
// — the predicate vocabulary still has to be visible without a
// list_attributes call.
func TestSearchToolDescriptionMentionsPredicates(t *testing.T) {
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

	tools, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var searchDesc string
	for _, tl := range tools.Tools {
		if tl.Name == "search" {
			searchDesc = tl.Description
			break
		}
	}
	if searchDesc == "" {
		t.Fatal("search tool not found in ListTools output")
	}

	// At minimum the description should mention these high-traffic
	// predicates inline; the full vocabulary lives in serverInstructions.
	for _, p := range []string{"is_markdown", "is_pdf", "is_image", "is_audio", "is_video", "is_source"} {
		if !strings.Contains(searchDesc, p) {
			t.Errorf("search tool description missing predicate %q", p)
		}
	}
}

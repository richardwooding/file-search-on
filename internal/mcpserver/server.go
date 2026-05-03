// Package mcpserver exposes file-search-on as a Model Context Protocol server.
package mcpserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// SearchInput is the JSON-schema input for the `search` tool.
type SearchInput struct {
	Expr    string `json:"expr,omitempty" jsonschema:"CEL expression matched against file attributes (e.g. 'is_pdf && page_count > 10'). Empty means match all."`
	Dir     string `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'."`
	Workers int    `json:"workers,omitempty" jsonschema:"Number of parallel workers. Defaults to runtime.NumCPU()."`
}

// SearchMatch is one match returned by the `search` tool.
type SearchMatch struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// SearchOutput is the structured output of the `search` tool.
type SearchOutput struct {
	Matches []SearchMatch `json:"matches"`
	Count   int           `json:"count"`
}

// ContentTypeDoc describes a registered content type.
type ContentTypeDoc struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// ListAttributesOutput is the structured output of the `list_attributes` tool.
type ListAttributesOutput struct {
	Schema       celexpr.SchemaDoc `json:"schema"`
	ContentTypes []ContentTypeDoc  `json:"content_types"`
}

// New builds an MCP server with file-search-on's tools registered. The
// server is not connected to a transport; callers either pass it to
// (*mcp.Server).Run for stdio service or (*mcp.Server).Connect for
// in-memory tests.
func New(version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "file-search-on",
		Version: version,
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Recursively search a directory for files matching a CEL expression evaluated over file metadata and content-type-specific attributes.",
	}, searchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attributes",
		Description: "List every CEL attribute available to the search tool, plus the registered content types.",
	}, listAttributesHandler)

	return s
}

// Run starts an MCP server on stdio and blocks until the transport closes
// or ctx is cancelled.
func Run(ctx context.Context, version string) error {
	return New(version).Run(ctx, &mcp.StdioTransport{})
}

func searchHandler(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	results, err := search.Walk(ctx, search.Options{
		Root:    dir,
		Expr:    expr,
		Workers: in.Workers,
	}, content.DefaultRegistry())
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("walk: %w", err)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })

	matches := make([]SearchMatch, len(results))
	for i, r := range results {
		matches[i] = SearchMatch{Path: r.Path, ContentType: r.ContentType, Size: r.Size}
	}

	return nil, SearchOutput{Matches: matches, Count: len(matches)}, nil
}

func listAttributesHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAttributesOutput, error) {
	types := content.DefaultRegistry().Types()
	docs := make([]ContentTypeDoc, len(types))
	for i, t := range types {
		docs[i] = ContentTypeDoc{Name: t.Name(), Extensions: t.Extensions()}
	}
	return nil, ListAttributesOutput{
		Schema:       celexpr.Schema(),
		ContentTypes: docs,
	}, nil
}

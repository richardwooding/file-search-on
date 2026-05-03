// Package mcpserver exposes file-search-on as a Model Context Protocol server.
package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// SearchInput is the JSON-schema input for the `search` tool.
type SearchInput struct {
	Expr         string `json:"expr,omitempty" jsonschema:"CEL expression matched against file attributes (e.g. 'is_pdf && page_count > 10'). Empty means match all."`
	Dir          string `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'."`
	Workers      int    `json:"workers,omitempty" jsonschema:"Number of parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes int    `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default; raise for very long log lines."`
}

// SearchMatch is one match returned by the `search` tool. Beyond path /
// content_type / size, every CEL-visible attribute is included when the
// matched content type emits it; absent fields are omitted from the JSON
// payload so simple consumers see a compact shape.
type SearchMatch struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`

	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Language string `json:"language,omitempty"`

	WordCount   int64 `json:"word_count,omitempty"`
	LineCount   int64 `json:"line_count,omitempty"`
	PageCount   int64 `json:"page_count,omitempty"`
	ColumnCount int64 `json:"column_count,omitempty"`

	CSVColumns  []string `json:"csv_columns,omitempty"`
	RootElement string   `json:"root_element,omitempty"`
	JSONKind    string   `json:"json_kind,omitempty"`

	ImgWidth  int64 `json:"img_width,omitempty"`
	ImgHeight int64 `json:"img_height,omitempty"`

	CameraMake   string  `json:"camera_make,omitempty"`
	CameraModel  string  `json:"camera_model,omitempty"`
	Lens         string  `json:"lens,omitempty"`
	TakenAt      string  `json:"taken_at,omitempty"` // RFC3339 when set
	Orientation  int64   `json:"orientation,omitempty"`
	GPSLat       float64 `json:"gps_lat,omitempty"`
	GPSLon       float64 `json:"gps_lon,omitempty"`
	ISO          int64   `json:"iso,omitempty"`
	FocalLength  float64 `json:"focal_length,omitempty"`
	FStop        float64 `json:"f_stop,omitempty"`
	ExposureTime float64 `json:"exposure_time,omitempty"`

	FrontmatterFormat string         `json:"frontmatter_format,omitempty"`
	Frontmatter       map[string]any `json:"frontmatter,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	Categories        []string       `json:"categories,omitempty"`
	Draft             bool           `json:"draft,omitempty"`
	Date              string         `json:"date,omitempty"` // RFC3339 when set

	IsMarkdown bool `json:"is_markdown,omitempty"`
	IsJSON     bool `json:"is_json,omitempty"`
	IsXML      bool `json:"is_xml,omitempty"`
	IsHTML     bool `json:"is_html,omitempty"`
	IsPDF      bool `json:"is_pdf,omitempty"`
	IsImage    bool `json:"is_image,omitempty"`
	IsText     bool `json:"is_text,omitempty"`
	IsCSV      bool `json:"is_csv,omitempty"`
	IsEPUB     bool `json:"is_epub,omitempty"`
	IsOffice   bool `json:"is_office,omitempty"`
}

// matchFrom projects a search.Result (with Attrs populated) into a
// SearchMatch wire object.
func matchFrom(r search.Result) SearchMatch {
	m := SearchMatch{
		Path:        r.Path,
		ContentType: r.ContentType,
		Size:        r.Size,
	}
	if r.Attrs == nil {
		return m
	}
	a := r.Attrs
	m.IsMarkdown, m.IsJSON, m.IsXML, m.IsHTML = a.IsMarkdown, a.IsJSON, a.IsXML, a.IsHTML
	m.IsPDF, m.IsImage = a.IsPDF, a.IsImage
	m.IsText, m.IsCSV, m.IsEPUB, m.IsOffice = a.IsText, a.IsCSV, a.IsEPUB, a.IsOffice

	if a.Extra == nil {
		return m
	}
	if v, ok := a.Extra["title"].(string); ok {
		m.Title = v
	}
	if v, ok := a.Extra["author"].(string); ok {
		m.Author = v
	}
	if v, ok := a.Extra["language"].(string); ok {
		m.Language = v
	}
	if v, ok := a.Extra["word_count"].(int64); ok {
		m.WordCount = v
	}
	if v, ok := a.Extra["line_count"].(int64); ok {
		m.LineCount = v
	}
	if v, ok := a.Extra["page_count"].(int64); ok {
		m.PageCount = v
	}
	if v, ok := a.Extra["column_count"].(int64); ok {
		m.ColumnCount = v
	}
	if v, ok := a.Extra["csv_columns"].([]string); ok {
		m.CSVColumns = v
	}
	if v, ok := a.Extra["root_element"].(string); ok {
		m.RootElement = v
	}
	if v, ok := a.Extra["kind"].(string); ok {
		m.JSONKind = v
	}
	if v, ok := a.Extra["width"].(int64); ok {
		m.ImgWidth = v
	}
	if v, ok := a.Extra["height"].(int64); ok {
		m.ImgHeight = v
	}
	if v, ok := a.Extra["camera_make"].(string); ok {
		m.CameraMake = v
	}
	if v, ok := a.Extra["camera_model"].(string); ok {
		m.CameraModel = v
	}
	if v, ok := a.Extra["lens"].(string); ok {
		m.Lens = v
	}
	if v, ok := a.Extra["taken_at"].(time.Time); ok && !v.IsZero() {
		m.TakenAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["orientation"].(int64); ok {
		m.Orientation = v
	}
	if v, ok := a.Extra["gps_lat"].(float64); ok {
		m.GPSLat = v
	}
	if v, ok := a.Extra["gps_lon"].(float64); ok {
		m.GPSLon = v
	}
	if v, ok := a.Extra["iso"].(int64); ok {
		m.ISO = v
	}
	if v, ok := a.Extra["focal_length"].(float64); ok {
		m.FocalLength = v
	}
	if v, ok := a.Extra["f_stop"].(float64); ok {
		m.FStop = v
	}
	if v, ok := a.Extra["exposure_time"].(float64); ok {
		m.ExposureTime = v
	}
	if v, ok := a.Extra["frontmatter_format"].(string); ok {
		m.FrontmatterFormat = v
	}
	if v, ok := a.Extra["frontmatter"].(map[string]any); ok && len(v) > 0 {
		m.Frontmatter = v
	}
	if v, ok := a.Extra["tags"].([]string); ok && len(v) > 0 {
		m.Tags = v
	}
	if v, ok := a.Extra["categories"].([]string); ok && len(v) > 0 {
		m.Categories = v
	}
	if v, ok := a.Extra["draft"].(bool); ok {
		m.Draft = v
	}
	if v, ok := a.Extra["date"].(time.Time); ok && !v.IsZero() {
		m.Date = v.Format(time.RFC3339)
	}
	return m
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
		Root:              dir,
		Expr:              expr,
		Workers:           in.Workers,
		MaxLineBytes:      in.MaxLineBytes,
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("walk: %w", err)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })

	matches := make([]SearchMatch, len(results))
	for i, r := range results {
		matches[i] = matchFrom(r)
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

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReadAttributesInput is the JSON-schema input for the `read_attributes`
// tool. Path can be absolute or relative to the server's working
// directory; agents should prefer absolute paths.
type ReadAttributesInput struct {
	Path   string   `json:"path" jsonschema:"Filesystem path of a single file to extract attributes from. Absolute paths are preferred; relative paths resolve against the server's working directory."`
	Fields []string `json:"fields,omitempty" jsonschema:"Project the response to only the listed attribute names — saves tokens when only a few attributes matter. 'path', 'content_type', and 'size' are always included regardless. Empty / omitted returns every populated attribute. Same field-name vocabulary as the search tool's 'fields' input; unknown names error at request validation time."`
}

func (h *handlers) readAttributesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadAttributesInput) (*mcp.CallToolResult, search.Match, error) {
	if in.Path == "" {
		return nil, search.Match{}, fmt.Errorf("path is required")
	}
	if err := search.ValidateFields(in.Fields); err != nil {
		return nil, search.Match{}, fmt.Errorf("fields: %w", err)
	}
	path, err := expandHomeDir(in.Path)
	if err != nil {
		return nil, search.Match{}, fmt.Errorf("expand path: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, search.Match{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Single-file extraction is bounded but not free (markdown reads
	// the whole file; PDFs / EXIF are header-only). Apply the server
	// default timeout so a pathological file can't wedge the server.
	// No per-call override on this tool — pass nil.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, nil)
	defer cancel()

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{Index: h.idx})
	if err != nil {
		return nil, search.Match{}, fmt.Errorf("read attributes: %w", err)
	}
	m := search.MatchFrom(search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	})
	return nil, search.ProjectMatch(m, in.Fields), nil
}

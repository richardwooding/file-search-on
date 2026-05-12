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
	Path string `json:"path" jsonschema:"Filesystem path of a single file to extract attributes from. Absolute paths are preferred; relative paths resolve against the server's working directory."`
}

func (h *handlers) readAttributesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadAttributesInput) (*mcp.CallToolResult, SearchMatch, error) {
	if in.Path == "" {
		return nil, SearchMatch{}, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return nil, SearchMatch{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Single-file extraction is bounded but not free (markdown reads
	// the whole file; PDFs / EXIF are header-only). Apply the server
	// default timeout so a pathological file can't wedge the server.
	if h.defaultTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.defaultTimeout)
		defer cancel()
	}

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{Index: h.idx})
	if err != nil {
		return nil, SearchMatch{}, fmt.Errorf("read attributes: %w", err)
	}
	return nil, matchFrom(search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	}), nil
}

package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// ContentTypeDoc describes a registered content type.
type ContentTypeDoc struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// ListAttributesOutput is the structured output of the `list_attributes` tool.
type ListAttributesOutput struct {
	CommonOutput
	Schema       celexpr.SchemaDoc `json:"schema"`
	ContentTypes []ContentTypeDoc  `json:"content_types"`
}

func (h *handlers) listAttributesHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAttributesOutput, error) {
	types := content.DefaultRegistry().Types()
	docs := make([]ContentTypeDoc, len(types))
	for i, t := range types {
		docs[i] = ContentTypeDoc{Name: t.Name(), Extensions: t.Extensions()}
	}
	return nil, ListAttributesOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Schema:       celexpr.Schema(),
		ContentTypes: docs,
	}, nil
}

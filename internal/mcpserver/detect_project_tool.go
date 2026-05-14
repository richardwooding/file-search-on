package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

// DetectProjectInput is the JSON-schema input for `detect_project`.
type DetectProjectInput struct {
	Dir string `json:"dir" jsonschema:"Directory to inspect. The directory's own listing is read once (non-recursive); indicator files are matched against basenames. Absolute paths preferred."`
}

// DetectProjectOutput surfaces the matched project types and the
// indicators that fired. Multiple types can match a single directory
// (e.g. a Go module with docker-compose.yml hits both).
type DetectProjectOutput struct {
	Path         string              `json:"path"`
	ProjectTypes []string            `json:"project_types"`
	Indicators   []projecttype.Match `json:"indicators"`
}

func (h *handlers) detectProjectHandler(ctx context.Context, _ *mcp.CallToolRequest, in DetectProjectInput) (*mcp.CallToolResult, DetectProjectOutput, error) {
	if in.Dir == "" {
		return nil, DetectProjectOutput{}, fmt.Errorf("dir is required")
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, DetectProjectOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, DetectProjectOutput{}, fmt.Errorf("resolve dir: %w", err)
	}

	// Honour the server's default timeout — single-dir listing is
	// cheap but a pathological filesystem could stall.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, nil)
	defer cancel()
	_ = ctx

	matches := projecttype.Detect(nil, abs)
	types := make([]string, len(matches))
	for i, m := range matches {
		types[i] = m.Type
	}
	return nil, DetectProjectOutput{
		Path:         abs,
		ProjectTypes: types,
		Indicators:   matches,
	}, nil
}

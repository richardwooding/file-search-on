package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

// ResolveProjectForPathInput is the JSON-schema input for the
// `resolve_project_for_path` tool.
type ResolveProjectForPathInput struct {
	Path string `json:"path" jsonschema:"Filesystem path of any file or directory. The walker walks up the directory chain (unbounded — terminates at the filesystem root) looking for the nearest ancestor that matches a registered project type (go.mod → go, package.json → node, Cargo.toml → rust, etc.). Absolute paths are preferred; relative paths resolve against the server's working directory."`
}

// ResolveProjectForPathOutput surfaces the matched project root, the
// matched type(s), and the indicator file(s) that fired. ProjectRoot
// is empty when no ancestor matches; ProjectTypes is then also empty.
// Multiple types can fire for the same root (a Go module that also
// ships docker-compose.yml hits both).
type ResolveProjectForPathOutput struct {
	CommonOutput
	Path         string              `json:"path"`
	ProjectRoot  string              `json:"project_root"`
	ProjectTypes []string            `json:"project_types"`
	Indicators   []projecttype.Match `json:"indicators,omitempty"`
}

func (h *handlers) resolveProjectForPathHandler(ctx context.Context, _ *mcp.CallToolRequest, in ResolveProjectForPathInput) (*mcp.CallToolResult, ResolveProjectForPathOutput, error) {
	if in.Path == "" {
		return nil, ResolveProjectForPathOutput{}, fmt.Errorf("path is required")
	}
	path, err := expandHomeDir(in.Path)
	if err != nil {
		return nil, ResolveProjectForPathOutput{}, fmt.Errorf("expand path: %w", err)
	}
	if path, err = h.validatePath(path); err != nil {
		return nil, ResolveProjectForPathOutput{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, ResolveProjectForPathOutput{}, fmt.Errorf("resolve path: %w", err)
	}

	// Honour the server default timeout. The walk-up is O(depth) ReadDir
	// calls so cheap in practice, but a pathological filesystem
	// (network mount stalled) could hang otherwise. No per-call override
	// — pass nil.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, nil)
	defer cancel()
	_ = ctx

	root, matches := projecttype.ResolveForPath(abs, nil)
	out := ResolveProjectForPathOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Path:         abs,
		ProjectRoot:  root,
	}
	if len(matches) > 0 {
		types := make([]string, len(matches))
		for i, m := range matches {
			types[i] = m.Type
		}
		out.ProjectTypes = types
		out.Indicators = matches
	}
	return nil, out, nil
}

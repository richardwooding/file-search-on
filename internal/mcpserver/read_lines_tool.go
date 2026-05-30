package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/search"
)

// ReadLinesInput is the JSON-schema input for the `read_lines` tool.
type ReadLinesInput struct {
	Path      string `json:"path" jsonschema:"Filesystem path of the file to read. Absolute paths preferred; relative resolves against the server's working directory."`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line to return (1-indexed, inclusive). Defaults to 1."`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Last line to return (1-indexed, inclusive). 0 means 'to end of file'. Defaults to 0."`
	MaxLines  int    `json:"max_lines,omitempty" jsonschema:"Cap on lines returned. Defaults to 1000. When the requested range exceeds the cap, truncated=true and only the first max_lines of the range are returned."`
}

// ReadLinesOutput is the structured output of `read_lines`. Lines
// excludes trailing newlines; TotalLines is always populated so
// agents can decide whether to fetch additional ranges.
type ReadLinesOutput struct {
	CommonOutput
	Path       string   `json:"path"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	TotalLines int      `json:"total_lines"`
	Lines      []string `json:"lines"`
	Truncated  bool     `json:"truncated,omitempty"`
}

func (h *handlers) readLinesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadLinesInput) (*mcp.CallToolResult, ReadLinesOutput, error) {
	if in.Path == "" {
		return nil, ReadLinesOutput{}, fmt.Errorf("path is required")
	}
	path, err := expandHomeDir(in.Path)
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("expand path: %w", err)
	}
	if path, err = h.validatePath(path); err != nil {
		return nil, ReadLinesOutput{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Honour the server's default timeout so a pathological file
	// (multi-gigabyte log) can't wedge the server. read_lines is
	// bounded by max_lines too, but the line scanner can still
	// take real time on huge files. No per-call override on this
	// tool — pass nil.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, nil)
	defer cancel()

	res, err := search.ReadLines(ctx, os.DirFS(dir), base, in.StartLine, in.EndLine, in.MaxLines)
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("read lines: %w", err)
	}
	return nil, ReadLinesOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Path:         abs,
		StartLine:    res.StartLine,
		EndLine:      res.EndLine,
		TotalLines:   res.TotalLines,
		Lines:        res.Lines,
		Truncated:    res.Truncated,
	}, nil
}

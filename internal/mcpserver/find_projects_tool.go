package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/projectdetect"
)

// FindProjectsInput is the JSON-schema input for `find_projects`.
type FindProjectsInput struct {
	Dir              string   `json:"dir" jsonschema:"Root directory to walk. Each directory whose contents match a registered project-type indicator (go.mod, package.json, Cargo.toml, *.tf, etc.) is reported as a project root."`
	Types            []string `json:"types,omitempty" jsonschema:"Optional filter — only return projects matching at least one of the named types (e.g. ['go','rust']). Empty means accept all built-in types: go, node, rust, python, ruby, java-maven, java-gradle, dotnet, terraform, docker-compose."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Basename globs to prune during the walk. Common values: ['node_modules', '.git', 'target', 'dist', 'vendor']."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse .gitignore at the walk root and skip matching paths."`
	Nested           bool     `json:"nested,omitempty" jsonschema:"When true, keep descending into matched project roots so nested sub-projects (monorepo workspaces, vendored deps) are also reported. Default false: stop at the first match per branch, which is the typical 'find me all my Go repos' shape."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Positive = seconds, 0 = no timeout, omitted = server default. On expiry the partial result is returned with cancelled=true."`
}

// FindProjectsOutput is the structured output of `find_projects`.
// Mirrors the partial-result contract used by search / stats /
// find_duplicates: cancellation is reported via the Cancelled flag
// rather than an error.
type FindProjectsOutput struct {
	CommonOutput
	Projects           []projectdetect.FoundProject `json:"projects"`
	Count              int                        `json:"count"`
	Cancelled          bool                       `json:"cancelled,omitempty"`
	CancellationReason string                     `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64                    `json:"elapsed_seconds,omitempty"`
}

func (h *handlers) findProjectsHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindProjectsInput) (*mcp.CallToolResult, FindProjectsOutput, error) {
	if in.Dir == "" {
		return nil, FindProjectsOutput{}, fmt.Errorf("dir is required")
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, FindProjectsOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, FindProjectsOutput{}, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, FindProjectsOutput{}, fmt.Errorf("resolve dir: %w", err)
	}

	// Resolve effective timeout via the shared helper, same as the
	// other walking tools. Pass timeout into Find via FindOptions
	// rather than wrapping ctx, so Find can stamp the right
	// CancellationReason.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	opts := projectdetect.FindOptions{
		Types:            in.Types,
		Excludes:         in.Excludes,
		Nested:           in.Nested,
		RespectGitignore: in.RespectGitignore,
	}
	if in.TimeoutSeconds != nil && *in.TimeoutSeconds > 0 {
		opts.Timeout = time.Duration(*in.TimeoutSeconds * float64(time.Second))
	}

	res, err := projectdetect.Find(ctx, abs, opts)
	if err != nil {
		return nil, FindProjectsOutput{}, fmt.Errorf("find_projects: %w", err)
	}
	out := FindProjectsOutput{
		CommonOutput:       CommonOutput{ServerVersion: h.version},
		Projects:           res.Projects,
		Count:              res.Count,
		Cancelled:          res.Cancelled,
		CancellationReason: res.CancellationReason,
		ElapsedSeconds:     res.ElapsedSeconds,
	}
	return nil, out, nil
}

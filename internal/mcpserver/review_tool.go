package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReviewInput is the JSON-schema input for the `review` tool. It reuses the
// shared code-graph walk surface and adds the diff-gate parameters (#484).
type ReviewInput struct {
	codeGraphWalkInput
	Base          string `json:"base,omitempty" jsonschema:"Git ref to diff against. Empty (default) reviews uncommitted changes vs HEAD (pre-commit). A ref (e.g. 'origin/main') reviews <base>...HEAD — the changes introduced on HEAD since its merge-base with <base> (PR gate). 'dir' is the git working directory."`
	MaxComplexity int    `json:"max_complexity,omitempty" jsonschema:"Cyclomatic-complexity ceiling for a function in a changed file; functions above it are a fail-level finding. Defaults to 15."`
	SkipDeadCode  bool   `json:"skip_dead_code,omitempty" jsonschema:"When true, skip the dead-code check (it adds a second graph pass). Dead-code findings are warn-level."`
}

// ReviewOutput is the diff-scoped review verdict + findings.
type ReviewOutput struct {
	CommonOutput
	Base               string                 `json:"base"`
	ChangedFiles       []string               `json:"changed_files"`
	FilesAnalysed      int                    `json:"files_analysed"`
	Findings           []search.ReviewFinding `json:"findings"`
	Verdict            string                 `json:"verdict"`
	WarnCount          int                    `json:"warn_count"`
	FailCount          int                    `json:"fail_count"`
	Cancelled          bool                   `json:"cancelled,omitempty"`
	CancellationReason string                 `json:"cancellation_reason,omitempty"`
}

func (h *handlers) reviewHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReviewInput) (*mcp.CallToolResult, ReviewOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, ReviewOutput{}, err
	}
	// Review treats the first root as the git working dir; collapse a single
	// 'dirs' entry to Root so reviewRoot resolves it consistently.
	if opts.Root == "" && len(opts.Roots) > 0 {
		opts.Root = opts.Roots[0]
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.Review(ctx, opts, content.DefaultRegistry(), search.ReviewConfig{
		Base:          in.Base,
		MaxComplexity: in.MaxComplexity,
		CheckDeadCode: !in.SkipDeadCode,
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, ReviewOutput{}, fmt.Errorf("review: %w", err)
	}

	out := ReviewOutput{ChangedFiles: []string{}, Findings: []search.ReviewFinding{}, Verdict: "pass"}
	if res != nil {
		out.Base = res.Base
		if res.ChangedFiles != nil {
			out.ChangedFiles = res.ChangedFiles
		}
		out.FilesAnalysed = res.FilesAnalysed
		if res.Findings != nil {
			out.Findings = res.Findings
		}
		out.Verdict = res.Verdict
		out.WarnCount = res.WarnCount
		out.FailCount = res.FailCount
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

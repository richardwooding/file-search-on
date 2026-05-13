package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// FindMatchesInput is the JSON-schema input for the `find_matches` tool.
type FindMatchesInput struct {
	Pattern             string   `json:"pattern" jsonschema:"RE2 regular expression matched against each line of every candidate file. RE2 is Google's regex flavour — same as Go's regexp/re2 package and CEL's matches(). Examples: 'TODO', '(?i)\\\\bpassword\\\\b', '^func '. Required; empty input returns an error."`
	Expr                string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope candidate files BEFORE regex scanning — same vocabulary as the search tool (is_source, is_markdown, language == \"go\", loc > 100, …). Empty means every file (filtered to text content types). Use a tight expr to prune large trees: 'is_source && language == \"go\"' before a heavy regex is dramatically cheaper than scanning every file."`
	Dir                 string   `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs                []string `json:"dirs,omitempty" jsonschema:"Multiple directories to search in one call. When non-empty, takes precedence over 'dir'."`
	Workers             int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes        int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML during walk-stage attribute extraction (bytes). 0 uses the 1 MiB default. Independent of the find_matches scan's own per-line cap (64 KiB)."`
	TimeoutSeconds      *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool: positive = seconds, 0 = no timeout. On expiry the partial result set is returned with cancelled=true."`
	ContextBefore       int      `json:"context_before,omitempty" jsonschema:"Number of lines of leading context to attach to each match. 0 means no Before window."`
	ContextAfter        int      `json:"context_after,omitempty" jsonschema:"Number of lines of trailing context to attach to each match. 0 means no After window."`
	MaxMatchesPerFile   int      `json:"max_matches_per_file,omitempty" jsonschema:"Cap on matches reported per file. 0 = unlimited. The scan keeps reading past the cap until pending After windows are filled, so the last matches still carry full trailing context."`
	Excludes            []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned. Same semantics as search."`
	RespectGitignore    bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, pre-walks each root to discover project subdirectories and prunes the canonical build-artefact basenames (vendor / node_modules / target / __pycache__ / .terraform / …). Unioned with 'excludes'."`
}

// FindMatchesOutput is the structured response.
//
// Matches is sorted by (path, line) ascending. FilesScanned counts
// text-typed files that were opened and line-scanned;
// FilesWithMatches is the subset that produced at least one hit.
// Cancelled / CancellationReason mirror search / stats / duplicates.
type FindMatchesOutput struct {
	Matches            []LineMatch `json:"matches"`
	Count              int         `json:"count"`
	FilesScanned       int         `json:"files_scanned"`
	FilesWithMatches   int         `json:"files_with_matches"`
	Cancelled          bool        `json:"cancelled,omitempty"`
	CancellationReason string      `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64     `json:"elapsed_seconds,omitempty"`
}

// LineMatch is one hit returned by find_matches. Mirrors
// search.LineMatch but lives here so the MCP wire shape is owned by
// the mcpserver package and stays decoupled from internal struct
// changes downstream.
type LineMatch struct {
	Path        string   `json:"path"`
	ContentType string   `json:"content_type,omitempty"`
	Line        int      `json:"line"`
	Text        string   `json:"text"`
	Before      []string `json:"before,omitempty"`
	After       []string `json:"after,omitempty"`
}

func (h *handlers) findMatchesHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindMatchesInput) (*mcp.CallToolResult, FindMatchesOutput, error) {
	if in.Pattern == "" {
		return nil, FindMatchesOutput{}, fmt.Errorf("pattern is required")
	}
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir := in.Dir
	if dir == "" && len(in.Dirs) == 0 {
		dir = "."
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.FindMatches(ctx, search.Options{
		Root:                dir,
		Roots:               in.Dirs,
		Expr:                expr,
		Workers:             in.Workers,
		MaxLineBytes:        in.MaxLineBytes,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
		Pattern:             in.Pattern,
		ContextBefore:       in.ContextBefore,
		ContextAfter:        in.ContextAfter,
		MaxMatchesPerFile:   in.MaxMatchesPerFile,
	}, content.DefaultRegistry())

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindMatchesOutput{}, fmt.Errorf("find_matches: %w", err)
	}

	out := FindMatchesOutput{}
	if res != nil {
		out.Count = res.Count
		out.FilesScanned = res.FilesScanned
		out.FilesWithMatches = res.FilesWithMatches
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		out.ElapsedSeconds = res.ElapsedSeconds
		out.Matches = make([]LineMatch, len(res.Matches))
		for i, m := range res.Matches {
			out.Matches[i] = LineMatch{
				Path:        m.Path,
				ContentType: m.ContentType,
				Line:        m.Line,
				Text:        m.Text,
				Before:      m.Before,
				After:       m.After,
			}
		}
	}
	return nil, out, nil
}

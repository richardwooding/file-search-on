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
	Expr                string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope candidate files BEFORE regex scanning — same vocabulary as the search tool (is_source, is_markdown, language == \"go\", loc > 100, …). Empty means every file (plain-text types scanned directly; office / epub / pdf / email extracted to text and scanned; binary families skipped). Use a tight expr to prune large trees: 'is_source && language == \"go\"' before a heavy regex is dramatically cheaper than scanning every file."`
	Dir                 string   `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs                []string `json:"dirs,omitempty" jsonschema:"Multiple directories to search in one call. When non-empty, takes precedence over 'dir'."`
	Workers             int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes        int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML during walk-stage attribute extraction (bytes). 0 uses the 1 MiB default. Independent of the find_matches scan's own per-line cap (64 KiB)."`
	TimeoutSeconds      *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool: positive = seconds, 0 = no timeout. On expiry the partial result set is returned with cancelled=true."`
	ContextBefore       int      `json:"context_before,omitempty" jsonschema:"Number of lines of leading context to attach to each match. 0 means no Before window."`
	ContextAfter        int      `json:"context_after,omitempty" jsonschema:"Number of lines of trailing context to attach to each match. 0 means no After window."`
	MaxMatchesPerFile   int      `json:"max_matches_per_file,omitempty" jsonschema:"Cap on matches reported per file. 0 = unlimited. The scan keeps reading past the cap until pending After windows are filled, so the last matches still carry full trailing context."`
	MatchIn             string   `json:"match_in,omitempty" jsonschema:"Filter matches by per-line role. One of: 'any' (default — every regex hit), 'comments' (only hits on lines classified as a comment under the source file's language syntax: Go //, Python #, C /* */, plus block-comment continuation lines), 'code' (only hits on lines that AREN'T comments). Drops the typical TODO-sweep noise (test fixtures, string literals, fuzz seeds) without the agent having to hand-roll '^\\\\s*//<pattern>' regexes. Non-source files (markdown / json / plain text) are unaffected — they have no language syntax registered and the filter no-ops. Line-granular: a trailing-comment line like 'x := 1 // TODO' classifies as code. 'strings' mode (matching inside string literals) is deferred to a follow-up. Unknown values return an error. Issue #272."`
	Limit               int      `json:"limit,omitempty" jsonschema:"Cap the number of line matches returned in this page (ordered by path, then line). 0 = all. When the set is truncated, the response carries next_cursor — pass it back as 'cursor' to fetch the next page. Distinct from max_matches_per_file (a per-file cap applied during the scan)."`
	Cursor              string   `json:"cursor,omitempty" jsonschema:"Opaque pagination token from a previous response's next_cursor. Resumes the (path, line)-ordered match list after the last item of the prior page. Use the SAME pattern/expr/context for stable paging; the scan re-runs each page (attributes are cached, so it's cheap)."`
	Excludes            []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned. Same semantics as search."`
	RespectGitignore    bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks      bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, pre-walks each root to discover project subdirectories and prunes the canonical build-artefact basenames (vendor / node_modules / target / __pycache__ / .terraform / …). Unioned with 'excludes'."`
	IncludeGit          bool     `json:"include_git,omitempty" jsonschema:"When true, walk into .git directories. By default .git is pruned from every walk; set this to include it."`
}

// FindMatchesOutput is the structured response.
//
// Matches is sorted by (path, line) ascending. FilesScanned counts
// text-typed files that were opened and line-scanned;
// FilesWithMatches is the subset that produced at least one hit.
// Cancelled / CancellationReason mirror search / stats / duplicates.
type FindMatchesOutput struct {
	CommonOutput
	Matches          []LineMatch `json:"matches"`
	Count            int         `json:"count"`
	FilesScanned     int         `json:"files_scanned"`
	FilesWithMatches int         `json:"files_with_matches"`
	// TruncatedFiles names every file whose scanner hit the per-line
	// buffer cap (64 KiB) on at least one line — the over-cap suffix
	// wasn't scanned, so a regex match past the cap silently misses.
	// Pair with the corresponding suggestion entry. Issue #283.
	TruncatedFiles     []string `json:"truncated_files,omitempty"`
	// NextCursor is present only when the match list was truncated by
	// Limit and more hits remain. Count stays the total found across the
	// walk; Matches is the current page. Pass next_cursor back as
	// 'cursor' to fetch the next page. Issue #336.
	NextCursor         string   `json:"next_cursor,omitempty"`
	Cancelled          bool     `json:"cancelled,omitempty"`
	CancellationReason string   `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64  `json:"elapsed_seconds,omitempty"`
	// Suggestions populated on cancellation (#168 sub-feature C) AND
	// on truncation (#283).
	Suggestions []string `json:"suggestions,omitempty"`
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
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, FindMatchesOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, FindMatchesOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, FindMatchesOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, FindMatchesOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, FindMatchesOutput{}, err
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.FindMatches(ctx, search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                expr,
		Workers:             in.Workers,
		MaxLineBytes:        in.MaxLineBytes,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
		IncludeGitDir:       in.IncludeGit,
		Pattern:             in.Pattern,
		ContextBefore:       in.ContextBefore,
		ContextAfter:        in.ContextAfter,
		MaxMatchesPerFile:   in.MaxMatchesPerFile,
		MatchIn:             in.MatchIn,
	}, content.DefaultRegistry())

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindMatchesOutput{}, fmt.Errorf("find_matches: %w", err)
	}

	out := FindMatchesOutput{}
	if res != nil {
		out.Count = res.Count
		out.FilesScanned = res.FilesScanned
		out.FilesWithMatches = res.FilesWithMatches
		out.TruncatedFiles = res.TruncatedFiles
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
		if out.Cancelled {
			// Project FilesWithMatches into a synthetic []search.Match so
			// the hot-directory heuristic works against the files that
			// produced hits. (find_matches doesn't carry full Match
			// objects; we already have per-line.Path which is enough.)
			pathSet := make(map[string]bool, out.FilesWithMatches)
			for _, m := range out.Matches {
				pathSet[m.Path] = true
			}
			synthMatches := make([]search.Match, 0, len(pathSet))
			for p := range pathSet {
				synthMatches = append(synthMatches, search.Match{Path: p})
			}
			out.Suggestions = search.SuggestionsForSearch(search.Options{
				Expr:             in.Expr,
				Excludes:         in.Excludes,
				RespectGitignore: in.RespectGitignore,
			}, synthMatches, out.ElapsedSeconds, out.CancellationReason)
		}
		// Truncation hint — independent of cancellation. When the
		// scanner blew past the 64 KiB per-line cap on any file, the
		// over-cap suffix didn't participate in the regex pass, so a
		// match past the cap would silently miss. Surface the file
		// count + a copy-pasteable suggestion. Issue #283.
		if len(out.TruncatedFiles) > 0 {
			out.Suggestions = append(out.Suggestions,
				fmt.Sprintf("%d file(s) had lines exceeding the 64 KiB scanner cap; the over-cap suffix wasn't scanned. Pre-split the offending files (e.g. `sed 's/[delimiter]/\\n/g'`) or use external grep for them.",
					len(out.TruncatedFiles)))
		}
	}

	// Cursor pagination over the (path, line)-ordered match list. Count
	// stays the total found across the walk; Matches becomes the page.
	// Computed AFTER the suggestion heuristics above so they see the full
	// match set. Issue #336.
	if in.Cursor != "" || in.Limit > 0 {
		page, next, perr := search.PaginateGeneric(out.Matches, func(m LineMatch) []any {
			return []any{m.Path, int64(m.Line)}
		}, []string{"asc", "asc"}, "find_matches:"+in.Pattern, in.Cursor, in.Limit)
		if perr != nil {
			return nil, FindMatchesOutput{}, fmt.Errorf("cursor: %w", perr)
		}
		out.Matches, out.NextCursor = page, next
	}

	out.ServerVersion = h.version
	return nil, out, nil
}

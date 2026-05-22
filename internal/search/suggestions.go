package search

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// SuggestionsForSearch inspects the cancelled walk state and returns
// agent-actionable hints for the next call. Heuristic and bounded —
// at most a handful of strings, each starting with a verb so an
// agent can pattern-match if it wants to automate.
//
// Issue #168 sub-feature C. Populated on the cancelled MCP responses
// (SearchOutput / FindMatchesOutput.Suggestions) and printed to
// stderr by the CLI on cancellation.
//
// Returns nil when none of the heuristics fire (uncommon — at least
// the bump-timeout suggestion fires on every "timeout" reason).
func SuggestionsForSearch(opts Options, matches []Match, elapsedSeconds float64, cancellationReason string) []string {
	var out []string
	out = appendBumpTimeoutSuggestion(out, elapsedSeconds, cancellationReason)
	out = appendHotDirectorySuggestion(out, matchPaths(matches))
	out = appendIncludeBodySuggestion(out, opts.IncludeBody)
	out = appendMissingPrunesSuggestion(out, opts.Excludes, opts.RespectGitignore)
	out = appendLaxFilterSuggestion(out, opts.Expr)
	return out
}

// SuggestionsForStats is the stats-shaped variant. The matches list
// isn't exposed by ComputeStats — every file walked contributes to
// the histogram — so the hot-directory heuristic is skipped (the
// histogram itself answers "what's in this tree?" better than any
// suggestion could). The remaining heuristics apply unchanged.
func SuggestionsForStats(opts Options, elapsedSeconds float64, cancellationReason string) []string {
	var out []string
	out = appendBumpTimeoutSuggestion(out, elapsedSeconds, cancellationReason)
	out = appendIncludeBodySuggestion(out, opts.IncludeBody)
	out = appendMissingPrunesSuggestion(out, opts.Excludes, opts.RespectGitignore)
	out = appendLaxFilterSuggestion(out, opts.Expr)
	return out
}

// appendBumpTimeoutSuggestion fires on cancellation_reason == "timeout".
// Suggests doubling the timeout, rounded up to a humane multiple of a
// second.
func appendBumpTimeoutSuggestion(out []string, elapsedSeconds float64, reason string) []string {
	if reason != "timeout" {
		return out
	}
	doubled := time.Duration(elapsedSeconds*2*float64(time.Second)) + time.Second
	// Round up to nearest second for a clean number.
	doubled = doubled.Round(time.Second)
	return append(out,
		fmt.Sprintf("Walk hit the timeout at %.1fs. Pass --timeout %s (≈2x current) for a longer run.",
			elapsedSeconds, doubled))
}

// appendHotDirectorySuggestion looks at the longest common prefix of
// match paths. When ≥2 matches share a common parent directory
// significantly more specific than the walk root would suggest, hint
// at narrowing the scope.
func appendHotDirectorySuggestion(out []string, paths []string) []string {
	if len(paths) < 2 {
		return out
	}
	prefix := longestCommonDirPrefix(paths)
	if prefix == "" || prefix == "/" || prefix == "." {
		return out
	}
	return append(out,
		fmt.Sprintf("All %d matches live under %s. Consider narrowing with -d %s (or --exclude '%s' for the inverse).",
			len(paths), prefix, prefix, path.Base(prefix)))
}

// appendIncludeBodySuggestion fires when IncludeBody is set. Body
// reads dominate walk time on lax filters — prompt the agent to add
// a type predicate.
func appendIncludeBodySuggestion(out []string, includeBody bool) []string {
	if !includeBody {
		return out
	}
	return append(out,
		"--body / include_body reads every candidate file's body. Add a tighter type predicate (is_markdown, is_source, is_office, …) to pre-prune before the body scan.")
}

// appendMissingPrunesSuggestion fires when neither --exclude nor
// --respect-gitignore is set. Common build / cache directories often
// dominate naive walks.
func appendMissingPrunesSuggestion(out []string, excludes []string, respectGitignore bool) []string {
	if len(excludes) > 0 || respectGitignore {
		return out
	}
	return append(out,
		"No --exclude or --respect-gitignore is set. Common build / cache dirs (node_modules, .git, target, __pycache__, vendor) may dominate the walk; pass --exclude or --respect-gitignore to prune them.")
}

// appendLaxFilterSuggestion fires when the CEL expression is empty
// or `true` — every file is walked. Suggest a type predicate.
func appendLaxFilterSuggestion(out []string, expr string) []string {
	trimmed := strings.TrimSpace(expr)
	if trimmed != "" && trimmed != "true" {
		return out
	}
	return append(out,
		"The CEL filter is empty or 'true' — every file is walked. Add a type predicate (is_pdf, is_image, is_source, …) to limit candidates.")
}

// matchPaths projects []Match to []string of paths. Kept tight so
// hot-directory analysis doesn't allocate via repeated field access.
func matchPaths(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Path
	}
	return out
}

// longestCommonDirPrefix returns the longest path prefix shared by
// every entry in paths. The iterative trim already drops trailing
// filename components (via LastIndexAny on `/` or `\`), so the
// returned value is always at a directory boundary. Returns "" when
// no useful prefix exists (empty input, no shared directory).
func longestCommonDirPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p, prefix) {
			i := strings.LastIndexAny(prefix, "/\\")
			if i < 0 {
				return ""
			}
			prefix = prefix[:i]
		}
	}
	return prefix
}

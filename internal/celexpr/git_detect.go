package celexpr

import "strings"

// gitAttrs lists every CEL attribute name whose value depends on the
// gitmeta cache. Used by NeedsGit to auto-enable --with-git when a
// caller's query references one of these. Keep in sync with the
// declarations in env.go / activation.go / schema.go (the four-place
// invariant covered by .claude/skills/extend-cel-schema).
var gitAttrs = [...]string{
	"git_last_commit_time",
	"git_last_commit_author",
	"git_last_commit_subject",
	"git_first_seen",
	"git_commit_count",
	"is_git_tracked",
	"is_git_ignored",
}

// NeedsGit reports whether any of the supplied CEL strings references
// a git_* / is_git_* attribute. Callers typically pass the user's
// CEL expression plus the sort_by and rank inputs in one call:
//
//	celexpr.NeedsGit(expr, sortBy, rank)
//
// Used by the CLI search subcommand and the MCP search tool to
// auto-enable WithGit so users don't have to remember the flag when
// their query already names git data. Same precedent as with_phash
// auto-enabling on `image_similar_to`.
//
// Implementation uses substring match rather than CEL-AST inspection:
// naive but cheap and consistent with the existing auto-detects. False
// positives are possible (e.g. an expression literal containing the
// substring `git_commit_count`) but cost only one unnecessary
// gitmeta.New() pass per walk — and on the MCP path the pool dedupes
// it across calls. False negatives would silently return empty results
// against git_* filters, which is the worse failure mode.
func NeedsGit(parts ...string) bool {
	for _, p := range parts {
		if p == "" {
			continue
		}
		for _, a := range gitAttrs {
			if strings.Contains(p, a) {
				return true
			}
		}
	}
	return false
}

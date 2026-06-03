package search

import (
	"fmt"
	"sort"
	"time"
)

// Preset is a named, pre-canned search recipe — a CEL filter
// expression plus sensible defaults for sort / rank / limit / opt-in
// flags. Defined statically in the catalog below; consumed by the
// CLI `preset` subcommand and the MCP `query_preset` / `list_presets`
// tools.
//
// Issue #168 sub-feature B.
type Preset struct {
	// Name is the canonical preset identifier — kebab_case, stable
	// across releases. Used by `preset <name>` and `query_preset`.
	Name string
	// Description is a one-line human summary shown by `preset` (no
	// args) and `list_presets`. Keep under 100 chars.
	Description string
	// Build constructs the per-invocation options. Called fresh at
	// each preset invocation so time-relative expressions (`mod_time >
	// timestamp(...)`) bake in the current `time.Now()` rather than
	// any global / package-init time.
	Build func() PresetOptions
}

// PresetOptions is the Preset.Build() output — the search.Options
// fields a preset cares about. Translated into a real search.Options
// by the caller; tests use it directly to compile-verify the CEL
// expression.
type PresetOptions struct {
	// Expr is the CEL filter, e.g. `is_image && taken_at > timestamp("2025-...")`.
	Expr string
	// Sort is a recognised sort key (size / mod_time / name / etc.).
	Sort string
	// Order is "asc" or "desc"; empty means follow the Sort/Rank
	// defaults.
	Order string
	// RankExpr is the CEL ranking expression (issue #168 sub-A) when
	// the preset wants a custom sort key over an attribute. Most
	// presets use Sort instead.
	RankExpr string
	// Limit caps the buffered result set.
	Limit int

	// IncludeBody, ComputeHashes, CheckDisguised opt the preset into
	// the corresponding search.Options flags. Some presets (e.g.
	// failed_tests) need body access to filter; suspicious_files
	// needs disguise detection for the is_disguised predicate.
	IncludeBody    bool
	ComputeHashes  bool
	CheckDisguised bool
}

// presets is the catalog. Add new entries here; the CLI / MCP / docs
// pick them up automatically.
var presets = []Preset{
	{
		Name:        "recent_changes",
		Description: "Files modified in the last 7 days, newest first.",
		Build: func() PresetOptions {
			cutoff := time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
			return PresetOptions{
				Expr:  fmt.Sprintf(`mod_time > timestamp(%q)`, cutoff),
				Sort:  "mod_time",
				Order: "desc",
				Limit: 50,
			}
		},
	},
	{
		Name:        "recent_photos",
		Description: "Images taken in the last 30 days, newest first.",
		Build: func() PresetOptions {
			cutoff := time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
			return PresetOptions{
				Expr:  fmt.Sprintf(`is_image && taken_at > timestamp(%q)`, cutoff),
				Sort:  "taken_at",
				Order: "desc",
				Limit: 50,
			}
		},
	},
	{
		Name:        "old_drafts",
		Description: "Markdown drafts not modified in the last 90 days — neglected work.",
		Build: func() PresetOptions {
			cutoff := time.Now().Add(-90 * 24 * time.Hour).Format(time.RFC3339)
			return PresetOptions{
				Expr:  fmt.Sprintf(`is_markdown && draft && mod_time < timestamp(%q)`, cutoff),
				Sort:  "mod_time",
				Order: "asc",
			}
		},
	},
	{
		Name:        "large_files",
		Description: "Files larger than 100 MB across all formats, largest first.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:  `size > 100000000`,
				Sort:  "size",
				Order: "desc",
				Limit: 20,
			}
		},
	},
	{
		Name:        "large_binaries",
		Description: "Compiled binaries larger than 100 MB, largest first — common disk-eater hunt.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:  `is_binary && size > 100000000`,
				Sort:  "size",
				Order: "desc",
				Limit: 20,
			}
		},
	},
	{
		Name:        "suspicious_files",
		Description: "Forensic triage shortcut — disguised files (magic disagrees with extension) or btime anomalies (file placed after being modified elsewhere).",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:           `is_disguised || is_btime_anomaly`,
				CheckDisguised: true,
			}
		},
	},
	{
		Name:        "failed_tests",
		Description: "Source-code test files with FAIL / FIXME / XXX / TODO in COMMENTS — code-review prompt.",
		Build: func() PresetOptions {
			// Issue #280: the original `body.matches("FAIL|FIXME|XXX")`
			// fired on every test-fixture string literal containing
			// those tokens (the headline #2 dogfood pain). Anchor the
			// match on a line that BEGINS with a common comment
			// marker (// for C-family, # for hash-family, -- for
			// Lua/SQL/Haskell, ; for Clojure/asm) followed by the
			// keyword. Uses RE2's (?m) flag so `^` matches each
			// line boundary against the full file body.
			//
			// Mixed-content lines like `assert(x == 1) // FIXME` are
			// NOT picked up — same shape as find_matches's
			// match_in=comments (issue #272). Acceptable: the
			// targeted reviewer signal is "comment-line annotation",
			// not trailing inline comments.
			return PresetOptions{
				Expr:        `is_source && is_test_file && body.matches("(?m)^\\s*(//|#|--|;)\\s*\\b(FAIL|FIXME|XXX|TODO)\\b")`,
				IncludeBody: true,
			}
		},
	},
	{
		Name:        "system_metadata",
		Description: "OS-generated leftover files — .DS_Store / Thumbs.db / Desktop.ini / .directory / .localized. Useful for cleanup.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr: `is_system_metadata`,
			}
		},
	},
	{
		Name:        "recent_commits",
		Description: "Files most-recently committed in the current git tree (last 7 days, newest first). Repo-aware sibling of recent_changes — fixes the 'fresh clone has all mtimes set to checkout time' problem the filesystem version has on repos.",
		Build: func() PresetOptions {
			cutoff := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
			return PresetOptions{
				Expr:  fmt.Sprintf(`git_last_commit_time > timestamp(%q)`, cutoff),
				Sort:  "git_last_commit_time",
				Order: "desc",
				Limit: 50,
			}
		},
	},
	{
		Name:        "hot_files",
		Description: "20 highest-churn files by git commit count, descending — refactor / review prioritisation. Tracks any git-tracked file (source, docs, config, data), not just source code. Repo-aware; relies on the git_commit_count CEL attribute (#271).",
		Build: func() PresetOptions {
			return PresetOptions{
				// Filter to git-tracked so untracked / vendor noise doesn't
				// dominate the top-N — but NOT to is_source, so high-churn
				// docs (markdown), config, and data files surface too.
				// git_commit_count > 0 forces git auto-warm via
				// celexpr.NeedsGit (a single attribute reference is enough)
				// and excludes untracked files.
				Expr:  `is_git_tracked && git_commit_count > 0`,
				Sort:  "git_commit_count",
				Order: "desc",
				Limit: 20,
			}
		},
	},
	{
		Name:        "prod_code",
		Description: "Production source code — tracked in git, not a test file, not machine-generated. The 'show me what humans wrote' filter. Composites #271 (is_git_tracked) + #276 (is_generated_code) + the per-language test convention.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr: `is_source && is_git_tracked && !is_test_file && !is_generated_code`,
				Sort: "loc",
				// Largest production files first — usually the most
				// interesting under a 'show me production code' frame
				// (entry points, long-lived modules, central dispatchers).
				Order: "desc",
				Limit: 100,
			}
		},
	},
	{
		Name:        "untracked_code",
		Description: "Source files NOT in git AND not matched by .gitignore — the 'did I forget to commit?' check. Catches new files an operator added but didn't `git add`. Uses #271 git-aware attributes.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:  `is_source && !is_git_tracked && !is_git_ignored`,
				Sort:  "size",
				Order: "desc",
				Limit: 50,
			}
		},
	},
	{
		Name:        "generated_code",
		Description: "Machine-generated source files — protoc / mockery / easyjson / //go:generate output. Audit the codegen footprint or feed into refactor planning. Uses is_generated_code from #276.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:  `is_source && is_generated_code`,
				Sort:  "size",
				Order: "desc",
				Limit: 50,
			}
		},
	},
	{
		Name:        "test_files",
		Description: "Source files matching each language's test convention (*_test.go / test_*.py / *.test.{js,ts,tsx} / *Test.java / *_spec.rb …). Useful for test-coverage reconnaissance or triaging the test surface.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:  `is_source && is_test_file`,
				Sort:  "loc",
				Order: "desc",
				Limit: 50,
			}
		},
	},
}

// Presets returns the catalog sorted alphabetically by Name. Safe to
// call concurrently — the underlying slice is read-only.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PresetByName returns the preset with the given Name, or nil if
// none matches. Comparison is case-sensitive — preset names are
// canonical lowercase identifiers.
func PresetByName(name string) *Preset {
	for i := range presets {
		if presets[i].Name == name {
			return &presets[i]
		}
	}
	return nil
}

// PresetNames returns the preset names sorted alphabetically. Useful
// for shell completion / `--help` output.
func PresetNames() []string {
	out := make([]string, 0, len(presets))
	for _, p := range presets {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

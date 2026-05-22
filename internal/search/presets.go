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
		Description: "Source-code test files mentioning FAIL / FIXME / XXX in the body — code-review prompt.",
		Build: func() PresetOptions {
			return PresetOptions{
				Expr:        `is_source && is_test_file && body.matches("FAIL|FIXME|XXX")`,
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

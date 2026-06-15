package celexpr_test

import (
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// sourceDisplayToken maps each registered `source/<slug>` language to the
// exact token the is_source doc string uses for it. Several slugs share a
// token on purpose (javascript+typescript -> "JS/TS", c+cpp -> "C/C++").
//
// This map is the forcing function: when you `registerSource(...)` a new
// language, TestIsSourceDocCoversEveryRegisteredLanguage fails until you
// (a) add the slug here and (b) add its token to the is_source description
// in Schema() (schema.go). The is_source string feeds both the CLI --list
// output and the MCP list_attributes tool, and it has silently drifted
// before (C# and several others were missing) — this test pins it shut.
var sourceDisplayToken = map[string]string{
	"go":         "Go",
	"javascript": "JS/TS",
	"typescript": "JS/TS",
	"rust":       "Rust",
	"c":          "C/C++",
	"cpp":        "C/C++",
	"java":       "Java",
	"swift":      "Swift",
	"kotlin":     "Kotlin",
	"scala":      "Scala",
	"zig":        "Zig",
	"python":     "Python",
	"shell":      "Shell",
	"elixir":     "Elixir",
	"ruby":       "Ruby",
	"lua":        "Lua",
	"haskell":    "Haskell",
	"ocaml":      "OCaml",
	"clojure":    "Clojure",
	"csharp":     "C#",
	"php":        "PHP",
	"perl":       "Perl",
	"r":          "R",
	"ada":        "Ada",
	"sql":        "SQL",
	"vb":         "Visual Basic",
	"fortran":    "Fortran",
	"matlab":     "MATLAB",
	"assembly":   "Assembly",
	"pascal":     "Pascal",
}

// registeredSourceSlugs returns the language slug for every registered
// `source/<slug>` content type (e.g. "csharp" for "source/csharp").
func registeredSourceSlugs(t *testing.T) []string {
	t.Helper()
	var slugs []string
	for _, ct := range content.DefaultRegistry().Types() {
		if slug, ok := strings.CutPrefix(ct.Name(), "source/"); ok {
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		t.Fatal("no source/* content types registered; importing internal/content should trigger their init()")
	}
	return slugs
}

// isSourceDocTokens parses the comma-separated language tokens out of the
// is_source attribute description, i.e. the bit before the em dash in
// "true if source code (Go, Python, ... Pascal — content_type ...)".
func isSourceDocTokens(t *testing.T) map[string]bool {
	t.Helper()
	var desc string
	for _, a := range celexpr.Schema().Common {
		if a.Name == "is_source" {
			desc = a.Description
			break
		}
	}
	if desc == "" {
		t.Fatal("is_source attribute not found in Schema().Common")
	}
	open := strings.IndexByte(desc, '(')
	dash := strings.Index(desc, "—")
	if open < 0 || dash < 0 || dash < open {
		t.Fatalf("is_source description not in expected '(... — ...)' form: %q", desc)
	}
	tokens := map[string]bool{}
	for tok := range strings.SplitSeq(desc[open+1:dash], ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			tokens[tok] = true
		}
	}
	return tokens
}

// TestIsSourceDocCoversEveryRegisteredLanguage guards against the
// is_source doc string drifting out of sync with the registered source
// languages. Both directions are checked so adding or removing a language
// without updating the doc string fails CI.
func TestIsSourceDocCoversEveryRegisteredLanguage(t *testing.T) {
	slugs := registeredSourceSlugs(t)
	docTokens := isSourceDocTokens(t)

	// Forward: every registered language must have a mapping and its token
	// must appear in the doc string.
	claimed := map[string]bool{}
	for _, slug := range slugs {
		token, ok := sourceDisplayToken[slug]
		if !ok {
			t.Errorf("registered source language %q has no entry in sourceDisplayToken; add it here and to the is_source description in schema.go", slug)
			continue
		}
		if !docTokens[token] {
			t.Errorf("source language %q (token %q) is missing from the is_source description in schema.go", slug, token)
		}
		claimed[token] = true
	}

	// Reverse: every token in the doc string must be claimed by some
	// registered language — catches a language that was removed but left
	// dangling in the doc string.
	for token := range docTokens {
		if !claimed[token] {
			t.Errorf("is_source description lists %q but no registered source language maps to it; remove it from schema.go (or add the language)", token)
		}
	}
}

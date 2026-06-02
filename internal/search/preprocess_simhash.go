package search

import (
	"regexp"
	"strings"
)

// preprocessForFingerprint strips language-specific boilerplate from
// body before SimHash computation. The headline failure mode #274
// surfaced was 90 Go files lumped into one near-duplicate group at
// 75-90% similarity — driven entirely by structurally-identical
// scaffolding (license headers, `package` declarations, common
// `import (...)` blocks, error-handling idioms). Stripping that
// scaffolding lets SimHash see the file's actual content.
//
// Two passes:
//
//  1. Leading comment block: drop every comment / blank line from
//     the top of the file until the first real code line.
//     Eliminates SPDX / Apache / MIT license headers + project
//     copyright preambles, which are byte-identical across every
//     file in many trees.
//  2. Per-language package / import lines: drop the language's
//     module / namespace declaration plus its import statements
//     (including Go's parenthesised `import (...)` blocks).
//
// Returns body unchanged when contentType isn't a recognised source
// type — Markdown, JSON, etc. have no boilerplate concept and the
// preprocessor would do nothing useful.
func preprocessForFingerprint(body, contentType string) string {
	if body == "" {
		return body
	}
	language := languageFromContentType(contentType)
	if language == "" {
		return body
	}
	syntax, ok := commentSyntaxFor(language)
	if !ok {
		return body
	}
	body = stripLeadingBoilerplate(body, syntax)
	body = stripPackageAndImports(body, language)
	return body
}

// stripLeadingBoilerplate drops every comment / blank line from the
// top of body, stopping at the first non-comment / non-blank line.
// Uses the same classifyLine state machine that powers
// find_matches's --match-in=comments (issue #272) — so we get
// block-comment continuation handling for free.
func stripLeadingBoilerplate(body string, syntax commentSyntax) string {
	if body == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	inBlock := false
	cut := 0
	for i, line := range lines {
		// Treat whitespace-only as boilerplate-skippable.
		if strings.TrimSpace(line) == "" {
			cut = i + 1
			continue
		}
		role, newInBlock := classifyLine(line, syntax, inBlock)
		if role == roleComment {
			inBlock = newInBlock
			cut = i + 1
			continue
		}
		break
	}
	if cut >= len(lines) {
		return ""
	}
	return strings.Join(lines[cut:], "\n")
}

// stripPackageAndImports drops package-declaration + import lines
// per language. Each language's rules are simple enough to express
// as a few regexes; we don't try to parse the full grammar, just
// recognise the shapes that dominate the SimHash.
//
// Languages not covered here pass through unchanged — they'll still
// benefit from stripLeadingBoilerplate's comment-header removal,
// just without the imports trim. Add a case here when a new
// language hits the issue.
func stripPackageAndImports(body, language string) string {
	switch language {
	case "go":
		return stripGoPackageImports(body)
	case "python":
		return stripPythonImports(body)
	case "java", "kotlin", "scala", "groovy":
		return stripJavaStyleImports(body)
	case "rust":
		return stripRustUse(body)
	case "javascript", "typescript":
		return stripJSImports(body)
	case "c", "cpp", "csharp":
		return stripCStyleIncludes(body, language)
	}
	return body
}

// Go: drops `package <name>` and `import "..."` lines, plus the
// entire parenthesised `import (...)` block. The closing `)` is
// kept simple: we drop lines until we see the first `)` on a line
// of its own (matches gofmt'd output).
func stripGoPackageImports(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	inImportBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inImportBlock {
			if trimmed == ")" {
				inImportBlock = false
			}
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "package "):
			continue
		case strings.HasPrefix(trimmed, "import ("):
			inImportBlock = true
			continue
		case strings.HasPrefix(trimmed, "import \""), strings.HasPrefix(trimmed, "import `"):
			continue
		case reGoImportAliased.MatchString(trimmed):
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// reGoImportAliased matches `import foo "bar"` / `import . "bar"` /
// `import _ "bar"` single-line forms (rare but used; the SimHash
// would otherwise see "context" / "fmt" tokens from them).
var reGoImportAliased = regexp.MustCompile(`^import\s+\S+\s+["` + "`" + `]`)

// Python: drops `import x` and `from x import y` lines.
func stripPythonImports(body string) string {
	return stripByPrefix(body, []string{"import ", "from "})
}

// Java / Kotlin / Scala / Groovy: drops `package x;` and
// `import x;` lines.
func stripJavaStyleImports(body string) string {
	return stripByPrefix(body, []string{"package ", "import "})
}

// Rust: drops `use x;` lines (the only import shape).
func stripRustUse(body string) string {
	return stripByPrefix(body, []string{"use ", "extern crate "})
}

// JavaScript / TypeScript: drops ES-module `import` statements and
// CommonJS `const x = require("...")` lines.
func stripJSImports(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import "),
			strings.HasPrefix(trimmed, "export "),
			reJSRequire.MatchString(trimmed):
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// reJSRequire matches `const|let|var X = require("...")` shapes used
// by CommonJS callers.
var reJSRequire = regexp.MustCompile(`^(const|let|var)\s+\S+\s*=\s*require\(`)

// C / C++ / C#: drops `#include "..."` / `#include <...>` lines and
// (C#) `using X;` namespace lines.
func stripCStyleIncludes(body, language string) string {
	prefixes := []string{"#include"}
	if language == "csharp" {
		prefixes = append(prefixes, "using ", "namespace ")
	}
	return stripByPrefix(body, prefixes)
}

// stripByPrefix drops lines whose trimmed form starts with any of
// the given prefixes. The shared inner helper for languages whose
// rule is just "kill lines starting with X".
func stripByPrefix(body string, prefixes []string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		drop := false
		for _, p := range prefixes {
			if strings.HasPrefix(trimmed, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

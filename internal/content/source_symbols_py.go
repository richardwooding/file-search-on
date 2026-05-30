package content

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

var (
	// Top-level OR nested def at any indent. Surface nested defs too —
	// agents asking "is there a parse_args somewhere?" still want hits
	// when the symbol lives inside a class or function body.
	pyDefRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)
	// class Foo: or class Foo(Base):
	pyClassRe = regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)\s*[(:]`)
	// import foo / import foo.bar / import foo as bar — captures the
	// fully-qualified module on the left. Multi-name imports
	// ("import a, b") fall through to the comma-split path below.
	pyImportRe = regexp.MustCompile(`^\s*import\s+(.+?)(?:\s+as\s+\w+)?\s*$`)
	// from foo import X — record "foo" (the module), parallel to Go's
	// "we capture the import path, not the symbols pulled in".
	pyFromRe = regexp.MustCompile(`^\s*from\s+([A-Za-z_.][\w.]*)\s+import\b`)
	// Strip an inline `# comment` from the import-target portion.
	pyCommentRe = regexp.MustCompile(`\s*#.*$`)
)

// extractPythonSymbols scans Python source line-by-line. Captures:
//   - top-level + nested def (async-aware)
//   - top-level + nested class
//   - `import foo`, `import foo.bar`, `import foo as bar`, `import a, b`
//   - `from foo import …` — records "foo" (the module)
//
// Limitations (documented in source-code.md): does NOT understand
// string literals that contain `def …`, conditional imports inside
// `if TYPE_CHECKING:` (those are still surfaced — they look like
// imports — but in flow context they're conditional), or `__all__`
// re-exports. Best-effort regex, no AST.
func extractPythonSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := pyDefRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, m[1])
			continue
		}
		if m := pyClassRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := pyFromRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
			continue
		}
		if m := pyImportRe.FindStringSubmatch(line); m != nil {
			target := pyCommentRe.ReplaceAllString(m[1], "")
			for item := range strings.SplitSeq(target, ",") {
				name := strings.TrimSpace(item)
				if idx := strings.Index(name, " as "); idx >= 0 {
					name = strings.TrimSpace(name[:idx])
				}
				if name != "" && isValidPythonModuleName(name) {
					imports = append(imports, name)
				}
			}
		}
	}
	return
}

// isValidPythonModuleName returns true if s could be a Python module
// name — letters/digits/underscores separated by dots, leading char
// is letter or underscore. Filters out regex-false-positive hits like
// trailing parens, type-hint syntax, etc.
func isValidPythonModuleName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '.':
			if i == 0 {
				return false
			}
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			// always allowed
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

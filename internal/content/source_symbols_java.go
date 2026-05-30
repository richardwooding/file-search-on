package content

import (
	"bufio"
	"bytes"
	"regexp"
)

var (
	// Type declarations — class / interface / enum / record / @interface.
	// Accepts any combination of modifiers before the keyword (public,
	// abstract, sealed, etc.) and captures the type name.
	javaTypeRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|abstract|final|static|sealed|non-sealed|strictfp)\s+)*(?:class|interface|enum|record|@interface)\s+([A-Za-z_][\w$]*)`)
	// Imports — both regular and static. The `(?:static\s+)?` matches
	// `import static foo.Bar.method;`.
	javaImportRe = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.]+(?:\.\*)?)\s*;`)
	// Methods are harder to regex without an AST. The pattern matches:
	//   - indented line (4+ spaces or tab — methods inside a class)
	//   - optional modifier sequence
	//   - a return type (one identifier with optional generics / array)
	//   - a method name
	//   - parameter list opening "(" and closing ")"
	//   - either "{" or "throws … {"
	// Known false-positive shapes: control statements with parens,
	// constructors named the same as a type (those ARE caught — and
	// that's arguably correct).
	javaMethodRe = regexp.MustCompile(`^[ \t]+(?:(?:public|private|protected|static|final|abstract|synchronized|native|default|strictfp)\s+)+[\w<>\[\],.\s?]*?\s+([A-Za-z_][\w$]*)\s*\([^)]*\)\s*(?:throws\s+[\w.,\s]+)?\s*[\{;]`)
)

// extractJavaSymbols scans Java source line-by-line. Captures:
//   - top-level + nested class / interface / enum / record / @interface
//   - import statements (regular + static)
//   - methods (best-effort regex; see source-code.md for limitations)
//
// Limitations (documented): Java's grammar is hard to regex without
// an AST. Multi-line annotations, generic method returns that span
// lines, lambda expressions that look like method declarations — all
// are best-effort. Tests in source_symbols_java_test.go lock in the
// known false-positive shapes so v2 (tree-sitter-java) intentionally
// changes them. Agents that need rigorous Java parsing should pair
// these attributes with a separate AST tool.
func extractJavaSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := javaTypeRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := javaImportRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
			continue
		}
		if m := javaMethodRe.FindStringSubmatch(line); m != nil {
			// Guard against obvious false positives — keywords that
			// could appear in method-position. We never want `if`,
			// `for`, `while`, etc. captured as method names.
			name := m[1]
			if !isJavaKeyword(name) {
				functions = append(functions, name)
			}
		}
	}
	return
}

func isJavaKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "return", "throw", "throws",
		"new", "this", "super", "true", "false", "null",
		"try", "catch", "finally", "synchronized":
		return true
	}
	return false
}

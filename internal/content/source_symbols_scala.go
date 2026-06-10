package content

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

var (
	// Type declarations — class / trait / object / enum, with optional
	// `case` and a leading run of modifiers (private, sealed, abstract,
	// final, implicit, open, etc.). Captures the type identifier. Scala 3
	// `enum` is included. `case class` / `case object` are matched by the
	// optional `case` token in the modifier run.
	scalaTypeRe = regexp.MustCompile(`^\s*(?:(?:private|protected|public|sealed|abstract|final|implicit|open|case|lazy|override|transparent|inline)\s+)*(?:class|trait|object|enum)\s+([A-Za-z_$][\w$]*)`)
	// `def` declarations (methods + top-level functions). Allows a leading
	// run of modifiers, then captures the name token up to the first
	// `[` (type params), `(` (value params), `:` (result type), or `=`
	// (body / abstract default). The `\S+?` capture intentionally also
	// matches symbolic operator names (`+`, `>>=`) and backtick-quoted
	// names (`` `type` ``).
	scalaDefRe = regexp.MustCompile(`^\s*(?:(?:private|protected|public|override|final|implicit|inline|lazy|transparent|abstract|sealed)\s+)*def\s+(\S+?)\s*[\[(:=]`)
	// import a.b.c / import a.b._ / import a.b.* — captures the path
	// verbatim including a trailing wildcard. The grouped selector form
	// `import a.b.{X, Y}` is handled separately (prefix capture) because
	// `{` would otherwise leak into the path.
	scalaImportRe = regexp.MustCompile(`^\s*import\s+([\w.]+(?:\._|\.\*)?)\s*$`)
	// Grouped / selector import — `import a.b.{X, Y => Z, given}`. Captures
	// the package prefix `a.b`, mirroring PHP's grouped-import handling
	// (no per-symbol expansion).
	scalaImportGroupRe = regexp.MustCompile(`^\s*import\s+([\w.]+)\.\{`)
)

// extractScalaSymbols scans Scala source line-by-line. Captures:
//   - def declarations (methods + top-level functions, operator names included)
//   - class / case class / trait / object / case object / enum (Scala 3)
//   - import paths: `a.b.c`, wildcard `a.b._` / `a.b.*`, and the prefix of
//     grouped selectors `import a.b.{X, Y => Z}` → `a.b`
//
// Limitations (best-effort regex, no AST): definitions inside string
// literals or comments are not excluded; multi-line signatures are matched
// only on their opening line; grouped-import selectors collapse to their
// package prefix; `val` / `var` / `given` / `type`-alias declarations are
// not captured. Agents needing rigorous Scala parsing should pair these
// attributes with a dedicated AST tool.
func extractScalaSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := scalaTypeRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := scalaDefRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, strings.Trim(m[1], "`"))
			continue
		}
		if m := scalaImportGroupRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
			continue
		}
		if m := scalaImportRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
		}
	}
	return
}

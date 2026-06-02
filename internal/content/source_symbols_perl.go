package content

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

var (
	// sub name { ... } / sub name (signature) { ... } / sub name;
	// (predeclared). Top-level + nested. The name token must follow
	// `sub` on the same line.
	perlSubRe = regexp.MustCompile(`^\s*sub\s+([A-Za-z_]\w*)\b`)

	// package Foo::Bar; or package Foo::Bar { ... } (block form,
	// Perl 5.14+). The semicolon / opening brace terminator
	// distinguishes a package declaration from the `package`
	// keyword used in other contexts.
	perlPackageRe = regexp.MustCompile(`^\s*package\s+([\w:]+)\s*[;{]`)

	// Perl 5.38+ `class Foo` (use experimental 'class'), plus Moose
	// / Moo / Type::Tiny-style `class Foo` / `role Foo`.
	perlClassRe = regexp.MustCompile(`^\s*(?:class|role)\s+([\w:]+)\b`)

	// use Foo;        use Foo qw(bar);   use Foo::Bar 1.23 qw(...);
	// no Foo;         no warnings;
	// require Foo;    require Foo::Bar;
	// `use 5.010;` / `use strict;` etc. record their bareword
	// argument verbatim — agents querying `"strict" in imports`
	// match.
	perlUseRe = regexp.MustCompile(`^\s*(?:use|no|require)\s+([A-Za-z_][\w:]*)\b`)
)

// extractPerlSymbols scans Perl source line-by-line. Captures:
//   - top-level + nested `sub name { ... }` declarations (also
//     predeclarations `sub name;`).
//   - `package Foo::Bar;` and `package Foo::Bar { ... }` (block
//     form, 5.14+) — emitted as type_names since `package` is
//     Perl's closest analogue to a class / module name.
//   - Modern `class Foo` / `role Foo` (Perl 5.38+ experimental
//     class, plus Moose / Moo idioms) — emitted as type_names.
//   - `use Foo;` / `use Foo qw(...)`, `no Foo;`, `require Foo;` —
//     the bareword module name is recorded.
//
// POD blocks (`=pod ... =cut`, `=head1 ... =cut`, any `=word`
// directive) and the `__END__` / `__DATA__` markers terminate /
// suspend scanning so docs and trailing data don't false-match.
// POD opens on any line beginning with `=` and closes on a line
// beginning with `=cut`.
//
// Limitations (documented in source-code.md):
//   - Anonymous subs `my $cb = sub { ... }` have no name and are
//     correctly skipped.
//   - Multi-line `use` declarations only match on the line carrying
//     `use Module`; trailing qw() / version contents are ignored
//     (we capture the module name only).
//   - Heredoc bodies aren't recognised — if a heredoc contains
//     `sub foo { ... }` text it'll match. Rare in practice; agents
//     can name-filter.
func extractPerlSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	inPOD := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")

		// __END__ / __DATA__ markers terminate the source proper —
		// everything after is doc / trailing data, never code.
		if trimmed == "__END__" || trimmed == "__DATA__" {
			break
		}

		// POD-block awareness. A line starting with `=word` opens a
		// POD block; `=cut` closes it. Inside POD, skip everything.
		if inPOD {
			if strings.HasPrefix(trimmed, "=cut") {
				inPOD = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "=") && len(trimmed) > 1 && isPerlPODDirective(trimmed) {
			inPOD = true
			continue
		}

		if m := perlSubRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, m[1])
			continue
		}
		if m := perlPackageRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := perlClassRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := perlUseRe.FindStringSubmatch(line); m != nil {
			// Skip `use v5.36;` / `use v5.10.1;` version-string
			// shape. The regex captures `v5` (letter+digit) but
			// it's not a real module; agents querying imports
			// expect module names, not minimum-Perl-version
			// declarations.
			if isPerlVersionString(m[1]) {
				continue
			}
			imports = append(imports, m[1])
		}
	}
	return
}

// isPerlVersionString reports whether s looks like a Perl version
// literal of the shape `v<digit>...` (e.g. v5 / v5.36 / v5.10.1).
// Used by extractPerlSymbols to filter `use v5.36;` lines out of
// the imports list — those declare a minimum Perl version, not a
// module dependency.
func isPerlVersionString(s string) bool {
	if len(s) < 2 || (s[0] != 'v' && s[0] != 'V') {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isPerlPODDirective reports whether a line starting with `=`
// begins a POD directive (and therefore opens a POD block). POD
// directives include =pod / =head1 / =head2 / =over / =item /
// =back / =begin / =end / =for / =encoding / =cut. Excludes mere
// equals-prefixed code lines (rare but possible in unusual
// expressions like `=>` at column 0, though `=>` would need to be
// the LINE'S start — Perl style usually avoids that).
//
// We accept any `=<letter>...` shape — POD directives are by spec
// `=<letter>...`. This avoids false-firing on `==` / `=~` etc. at
// column 0.
func isPerlPODDirective(trimmed string) bool {
	if len(trimmed) < 2 {
		return false
	}
	c := trimmed[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

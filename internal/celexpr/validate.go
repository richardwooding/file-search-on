package celexpr

import (
	"regexp"
	"sort"
	"strings"
)

// ValidationResult is what ValidateExpr returns. OK reports whether
// the expression compiles; on failure, Error carries the cel-go
// message and (when the failure is an unknown-identifier) Suggestion
// carries a "did you mean 'X'?" hint computed by levenshtein against
// the known CEL surface (all declared variables + all built-in
// functions). ReferencedVariables / ReferencedFunctions are populated
// regardless of OK — the agent gets to see which names the expression
// REFERENCED (even if it didn't compile) so they can correlate against
// the schema. Issue #282.
type ValidationResult struct {
	OK                  bool
	Error               string
	ReferencedVariables []string
	ReferencedFunctions []string
	Suggestion          string
}

// ValidateExpr compiles expr through the same env New uses (so every
// declared CEL variable + every built-in function is in scope) and
// returns a structured validation outcome. No program is built — this
// is the pure compile-time check, an order of magnitude cheaper than
// a full search call.
//
// Used by the MCP validate_expr tool so agents can iterate on CEL
// without paying a walk's cost on every typo. Issue #282.
func ValidateExpr(expr string) ValidationResult {
	res := ValidationResult{}

	// Reference extraction runs independently of compile success — we
	// want to populate the lists even when the expression has typos.
	// Strip string literals first so a `body.contains("size")` doesn't
	// falsely surface "size" as a referenced variable.
	stripped := stripCELStringLiterals(expr)
	known := knownNames()
	idents := extractIdentifiers(stripped)
	for _, name := range idents {
		switch known[name] {
		case nameKindVar:
			res.ReferencedVariables = append(res.ReferencedVariables, name)
		case nameKindFunc:
			res.ReferencedFunctions = append(res.ReferencedFunctions, name)
		}
	}
	sort.Strings(res.ReferencedVariables)
	sort.Strings(res.ReferencedFunctions)

	// Compile via the same path New() uses. New returns a wrapped
	// error on compile failure; on success we still pay the env build
	// cost but skip the env.Program step entirely. The trade is one
	// extra env construction per validate call — fine for a CLI/MCP
	// validate tool that isn't on a hot path.
	_, err := New(expr)
	if err == nil {
		res.OK = true
		return res
	}
	res.Error = err.Error()
	if unknown := extractUnknownIdentifier(res.Error); unknown != "" {
		res.Suggestion = closestKnownName(unknown, known)
	}
	return res
}

// nameKind tags an identifier as a declared CEL variable or a
// registered function, to support partitioning during reference
// extraction.
type nameKind uint8

const (
	nameKindUnknown nameKind = iota
	nameKindVar
	nameKindFunc
)

// knownNames returns the union of every declared variable + every
// registered function, mapped to its kind. Built fresh per call —
// this is invoked only inside ValidateExpr (not a hot path).
func knownNames() map[string]nameKind {
	schema := Schema()
	out := make(map[string]nameKind, len(schema.Common)+len(schema.TypeSpecific)+len(schema.Frontmatter)+len(schema.Functions))
	for _, a := range schema.Common {
		out[a.Name] = nameKindVar
	}
	for _, a := range schema.TypeSpecific {
		out[a.Name] = nameKindVar
	}
	for _, a := range schema.Frontmatter {
		out[a.Name] = nameKindVar
	}
	for _, f := range schema.Functions {
		out[f.Name] = nameKindFunc
	}
	return out
}

// reCELIdent matches identifier-shaped tokens (`[A-Za-z_][A-Za-z0-9_]*`).
// Run against the literal-stripped expression so identifiers inside
// string literals don't survive as false positives.
var reCELIdent = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\b`)

func extractIdentifiers(s string) []string {
	matches := reCELIdent.FindAllStringSubmatch(s, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// stripCELStringLiterals zeros out the content of every "..." and
// '...' literal in expr so identifier extraction doesn't pick up
// names that happen to appear inside string content (e.g.
// `body.contains("size")` should NOT count "size" as a referenced
// variable). Naive: doesn't handle CEL's escape sequences perfectly
// (`\"` inside a string), but the failure mode is over-inclusion of
// identifiers — same shape as the body.contains hint in #281.
func stripCELStringLiterals(expr string) string {
	var b strings.Builder
	b.Grow(len(expr))
	i := 0
	for i < len(expr) {
		c := expr[i]
		if c == '"' || c == '\'' {
			b.WriteByte(c)
			quote := c
			i++
			for i < len(expr) && expr[i] != quote {
				// Skip escape sequences without copying.
				if expr[i] == '\\' && i+1 < len(expr) {
					i += 2
					continue
				}
				i++
			}
			b.WriteByte(quote)
			if i < len(expr) {
				i++ // consume closing quote
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// reCELUnknownIdent matches the cel-go error shape for an undeclared
// reference. cel-go's message reads roughly:
//
//	ERROR: <input>:1:42: undeclared reference to 'imprts' (in container '')
//
// We capture the bad identifier so closestKnownName can levenshtein-
// suggest the canonical spelling.
var reCELUnknownIdent = regexp.MustCompile(`undeclared reference to '([^']+)'`)

func extractUnknownIdentifier(errMsg string) string {
	m := reCELUnknownIdent.FindStringSubmatch(errMsg)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// closestKnownName returns a "did you mean 'X'?" suggestion when the
// unknown identifier is within levenshtein distance 2 of a known
// variable or function. Distance > 2 returns "" — at that point the
// suggestion is more confusing than helpful.
func closestKnownName(unknown string, known map[string]nameKind) string {
	if unknown == "" {
		return ""
	}
	bestDist := 3 // strictly < 3, so distance ≤ 2 wins
	best := ""
	for name := range known {
		d := Levenshtein(unknown, name)
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	if best == "" {
		return ""
	}
	return "did you mean '" + best + "'?"
}

package search

import "strings"

// lineRole is the per-line classification used by FindMatches when the
// caller passes a non-default MatchIn filter. roleCode is anything
// that isn't recognised as a comment; roleComment is a line whose
// content (after leading whitespace) starts with a line-comment marker
// OR a line that lies fully inside an open block-comment region. The
// v1 contract is line-granular — a trailing-comment line like
// `x := 1 // TODO` classifies as roleCode (matches the agent's hand-
// rolled `^\s*//` regex behaviour). Issue #272.
type lineRole uint8

const (
	roleCode lineRole = iota
	roleComment
)

// commentSyntax describes a language's comment markers. LinePrefixes
// lists every line-comment marker (most languages have one; PHP has
// `//` AND `#`). BlockStart / BlockEnd are the block-comment delimiters
// — empty when the language has none (Zig, Python, shell). Nested
// blocks (Haskell, OCaml, Rust) aren't supported in v1 — we treat the
// first BlockEnd as terminating the block.
type commentSyntax struct {
	LinePrefixes []string
	BlockStart   string
	BlockEnd     string
}

// languageSyntax maps the `language` attribute (the bare name without
// the source/ prefix) to its comment syntax. Covers the Tiobe top 20
// (May 2026) plus common adjacent languages. Languages absent from
// the map fall through to "no syntax known" — MatchIn filtering
// effectively becomes a no-op (every line stays roleCode) so the user
// gets the unfiltered result rather than a silent empty.
var languageSyntax = map[string]commentSyntax{
	// C-family: //, /* */
	"go":         {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"c":          {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"cpp":        {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"javascript": {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"typescript": {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"rust":       {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"java":       {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"csharp":     {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"swift":      {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"kotlin":     {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"scala":      {LinePrefixes: []string{"//"}, BlockStart: "/*", BlockEnd: "*/"},
	"zig":        {LinePrefixes: []string{"//"}}, // Zig has no block comments
	"php":        {LinePrefixes: []string{"//", "#"}, BlockStart: "/*", BlockEnd: "*/"},

	// Hash-family: #
	"python": {LinePrefixes: []string{"#"}},
	"ruby":   {LinePrefixes: []string{"#"}},
	"perl":   {LinePrefixes: []string{"#"}},
	"shell":  {LinePrefixes: []string{"#"}},
	"r":      {LinePrefixes: []string{"#"}},
	"elixir": {LinePrefixes: []string{"#"}},

	// Dash-family: --
	"sql":     {LinePrefixes: []string{"--"}, BlockStart: "/*", BlockEnd: "*/"},
	"lua":     {LinePrefixes: []string{"--"}, BlockStart: "--[[", BlockEnd: "]]"},
	"haskell": {LinePrefixes: []string{"--"}, BlockStart: "{-", BlockEnd: "-}"},
	"ada":     {LinePrefixes: []string{"--"}},

	// One-offs
	"clojure":  {LinePrefixes: []string{";"}},
	"vb":       {LinePrefixes: []string{"'"}},
	"matlab":   {LinePrefixes: []string{"%"}, BlockStart: "%{", BlockEnd: "%}"},
	"fortran":  {LinePrefixes: []string{"!"}},
	"ocaml":    {BlockStart: "(*", BlockEnd: "*)"},
	"pascal":   {LinePrefixes: []string{"//"}, BlockStart: "(*", BlockEnd: "*)"},
	"assembly": {LinePrefixes: []string{";", "#"}},
}

// commentSyntaxFor returns the syntax for language plus whether one is
// registered. The lookup is case-sensitive and matches the bare
// language attribute (e.g. "go" / "python"). Unknown languages return
// the zero syntax + false; callers treat this as "no filtering
// possible" — same effect as MatchIn="any".
func commentSyntaxFor(language string) (commentSyntax, bool) {
	s, ok := languageSyntax[language]
	return s, ok
}

// languageFromContentType extracts the bare language name from a
// content type like "source/go" → "go". Returns "" when the content
// type doesn't follow the source/* pattern (markdown, json, etc.) —
// non-source files have no commentSyntax, so MatchIn filtering does
// nothing for them (matches the issue's "ignored for non-source"
// contract).
func languageFromContentType(contentType string) string {
	rest, ok := strings.CutPrefix(contentType, "source/")
	if !ok {
		return ""
	}
	return rest
}

// classifyLine determines whether the given line is a comment under
// syntax, given whether we entered the line inside an unclosed block
// comment (inBlock). Returns the role plus the updated in-block state
// for the NEXT line.
//
// Decision rule (line-granular, v1):
//
//  1. If we entered inside a block and BlockEnd doesn't appear on
//     this line → roleComment, still inBlock.
//  2. If we entered inside a block and BlockEnd appears → roleComment;
//     check whether a new BlockStart appears AFTER the end to set the
//     next-line state.
//  3. Otherwise, strip leading whitespace and:
//     a. If the trimmed line starts with any LinePrefix → roleComment.
//     b. If it starts with BlockStart → roleComment; track whether the
//     block closes on the same line.
//     c. If it contains BlockStart somewhere but doesn't START with it
//     (mixed line like `x := 1; /* note */`) → roleCode; track block
//     state for trailing unclosed blocks.
//  4. Default → roleCode.
//
// Mixed lines (`x := 1 // TODO`) classify as roleCode — the same shape
// as the user's `^\s*//` hand-rolled regex.
func classifyLine(line string, syntax commentSyntax, inBlock bool) (lineRole, bool) {
	if inBlock {
		if syntax.BlockEnd != "" {
			if idx := strings.Index(line, syntax.BlockEnd); idx >= 0 {
				// Block ends on this line. Check for a new block-start
				// after the end.
				after := line[idx+len(syntax.BlockEnd):]
				stillInBlock := syntax.BlockStart != "" && blockStartUnclosed(after, syntax)
				return roleComment, stillInBlock
			}
		}
		// No end marker (or none on this line) → entire line is comment.
		return roleComment, true
	}

	trimmed := strings.TrimLeft(line, " \t")
	for _, prefix := range syntax.LinePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return roleComment, false
		}
	}

	if syntax.BlockStart == "" {
		return roleCode, false
	}

	// Block-start may appear anywhere on the line.
	idx := strings.Index(line, syntax.BlockStart)
	if idx < 0 {
		return roleCode, false
	}
	startsWithBlock := strings.HasPrefix(trimmed, syntax.BlockStart)
	after := line[idx+len(syntax.BlockStart):]
	endIdx := -1
	if syntax.BlockEnd != "" {
		endIdx = strings.Index(after, syntax.BlockEnd)
	}
	if endIdx >= 0 {
		// Block opens AND closes on this line. After-end content
		// determines the next-line state via blockStartUnclosed.
		afterEnd := after[endIdx+len(syntax.BlockEnd):]
		stillInBlock := blockStartUnclosed(afterEnd, syntax)
		if startsWithBlock {
			return roleComment, stillInBlock
		}
		return roleCode, stillInBlock
	}
	// Block opens, doesn't close on this line.
	if startsWithBlock {
		return roleComment, true
	}
	return roleCode, true
}

// blockStartUnclosed reports whether tail contains an unclosed block
// start (a BlockStart that isn't followed by BlockEnd before the end
// of tail). Used to track whether the next line begins inside a block.
func blockStartUnclosed(tail string, syntax commentSyntax) bool {
	if syntax.BlockStart == "" {
		return false
	}
	idx := strings.Index(tail, syntax.BlockStart)
	if idx < 0 {
		return false
	}
	if syntax.BlockEnd == "" {
		return true
	}
	rest := tail[idx+len(syntax.BlockStart):]
	return !strings.Contains(rest, syntax.BlockEnd)
}

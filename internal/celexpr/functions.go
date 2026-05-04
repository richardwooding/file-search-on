package celexpr

import (
	"unicode"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// fuzzyFunctions returns the cel.EnvOption set that registers fuzzy-string
// functions on the CEL environment. Adding a new function means: implement
// it below, declare it here, and add a FunctionDoc entry in schema.go's
// Schema(). All three sites move together — the Schema entry is what the
// CLI --list and the MCP list_attributes tool surface to clients.
func fuzzyFunctions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("levenshtein",
			cel.Overload("levenshtein_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(levenshteinBinding),
			),
		),
		cel.Function("soundex",
			cel.Overload("soundex_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(soundexBinding),
			),
		),
		cel.Function("ngrams",
			cel.Overload("ngrams_string_int",
				[]*cel.Type{cel.StringType, cel.IntType},
				cel.ListType(cel.StringType),
				cel.BinaryBinding(ngramsBinding),
			),
		),
		cel.Function("ngram_similarity",
			cel.Overload("ngram_similarity_string_string_int",
				[]*cel.Type{cel.StringType, cel.StringType, cel.IntType},
				cel.DoubleType,
				cel.FunctionBinding(ngramSimilarityBinding),
			),
		),
	}
}

// CEL bindings — adapt ref.Val arguments to Go primitives, dispatch to the
// pure algorithm, wrap the result back into a ref.Val.

func levenshteinBinding(a, b ref.Val) ref.Val {
	sa, ok := a.Value().(string)
	if !ok {
		return types.NewErr("levenshtein: expected string for arg 1, got %T", a.Value())
	}
	sb, ok := b.Value().(string)
	if !ok {
		return types.NewErr("levenshtein: expected string for arg 2, got %T", b.Value())
	}
	return types.Int(int64(Levenshtein(sa, sb)))
}

func soundexBinding(v ref.Val) ref.Val {
	s, ok := v.Value().(string)
	if !ok {
		return types.NewErr("soundex: expected string, got %T", v.Value())
	}
	return types.String(Soundex(s))
}

func ngramsBinding(a, b ref.Val) ref.Val {
	s, ok := a.Value().(string)
	if !ok {
		return types.NewErr("ngrams: expected string for arg 1, got %T", a.Value())
	}
	n, ok := b.Value().(int64)
	if !ok {
		return types.NewErr("ngrams: expected int for arg 2, got %T", b.Value())
	}
	return types.DefaultTypeAdapter.NativeToValue(Ngrams(s, int(n)))
}

func ngramSimilarityBinding(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("ngram_similarity: expected 3 args, got %d", len(args))
	}
	sa, ok := args[0].Value().(string)
	if !ok {
		return types.NewErr("ngram_similarity: expected string for arg 1, got %T", args[0].Value())
	}
	sb, ok := args[1].Value().(string)
	if !ok {
		return types.NewErr("ngram_similarity: expected string for arg 2, got %T", args[1].Value())
	}
	n, ok := args[2].Value().(int64)
	if !ok {
		return types.NewErr("ngram_similarity: expected int for arg 3, got %T", args[2].Value())
	}
	return types.Double(NgramSimilarity(sa, sb, int(n)))
}

// Algorithm implementations — exported so they can be unit-tested directly,
// without going through the CEL environment.

// Levenshtein returns the edit distance between a and b, counted in
// rune-level insertions, deletions, and substitutions. Case-sensitive.
// Two-row dynamic programming, O(len(a) * len(b)) time, O(min(len)) space.
func Levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	// Ensure rb is the shorter slice so the row buffer is small.
	if len(rb) > len(ra) {
		ra, rb = rb, ra
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,    // insertion
				prev[j]+1,      // deletion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// Soundex returns the American Soundex code (NARA standard) for the
// given word. The result is always a 4-character upper-case ASCII string
// of the form letter+digit+digit+digit. Non-letters are skipped. Empty
// or no-letter input returns "0000".
//
// Encoding map:
//
//	B F P V         -> 1
//	C G J K Q S X Z -> 2
//	D T             -> 3
//	L               -> 4
//	M N             -> 5
//	R               -> 6
//
// Vowels (A E I O U Y) are not emitted but reset the same-code
// suppression state, so consonant pairs separated by a vowel are
// preserved (e.g. "Robert" -> "R163"). H and W are transparent — they
// emit nothing AND leave the suppression state untouched, so consonants
// separated only by H/W collapse (e.g. "Ashcraft" -> "A261", because
// the H between S and C means the C's code-2 is treated as adjacent to
// S's code-2 and dropped). The first letter of the word's code seeds
// the suppression state, so Pfister -> P236 (the F is dropped because
// it shares P's code).
func Soundex(s string) string {
	const padded = "0000"
	letters := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters = append(letters, unicode.ToUpper(r))
		}
	}
	if len(letters) == 0 {
		return padded
	}
	first := letters[0]
	if first > 127 {
		// Non-ASCII letter — Soundex is ASCII-only; degrade gracefully.
		return padded
	}

	out := make([]byte, 0, 4)
	out = append(out, byte(first))
	prevCode := soundexCode(first)
	for _, r := range letters[1:] {
		if len(out) == 4 {
			break
		}
		if r == 'H' || r == 'W' {
			// Transparent: emit nothing, leave prevCode alone.
			continue
		}
		c := soundexCode(r)
		if c == 0 {
			// Vowel (A E I O U Y): reset suppression so the next
			// consonant always emits even if same code as previous.
			prevCode = 0
			continue
		}
		if c == prevCode {
			continue
		}
		out = append(out, '0'+byte(c))
		prevCode = c
	}
	for len(out) < 4 {
		out = append(out, '0')
	}
	return string(out)
}

func soundexCode(r rune) int {
	switch r {
	case 'B', 'F', 'P', 'V':
		return 1
	case 'C', 'G', 'J', 'K', 'Q', 'S', 'X', 'Z':
		return 2
	case 'D', 'T':
		return 3
	case 'L':
		return 4
	case 'M', 'N':
		return 5
	case 'R':
		return 6
	}
	return 0
}

// Ngrams returns the character-level n-grams of s as a slice of strings,
// in left-to-right order. Returns an empty slice (not nil) when n <= 0
// or n > rune-length of s. Rune-aware.
func Ngrams(s string, n int) []string {
	if n <= 0 {
		return []string{}
	}
	rs := []rune(s)
	if n > len(rs) {
		return []string{}
	}
	out := make([]string, 0, len(rs)-n+1)
	for i := 0; i+n <= len(rs); i++ {
		out = append(out, string(rs[i:i+n]))
	}
	return out
}

// NgramSimilarity returns the Jaccard similarity between the SETS of
// character n-grams of a and b: |A ∩ B| / |A ∪ B|. Returns 1.0 when
// both n-gram sets are empty (e.g. both strings empty or both shorter
// than n). Returns 0.0 when only one side is empty.
func NgramSimilarity(a, b string, n int) float64 {
	setA := ngramSet(a, n)
	setB := ngramSet(b, n)
	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}
	if len(setA) == 0 || len(setB) == 0 {
		return 0.0
	}
	intersect := 0
	for g := range setA {
		if _, ok := setB[g]; ok {
			intersect++
		}
	}
	union := len(setA) + len(setB) - intersect
	return float64(intersect) / float64(union)
}

func ngramSet(s string, n int) map[string]struct{} {
	out := map[string]struct{}{}
	for _, g := range Ngrams(s, n) {
		out[g] = struct{}{}
	}
	return out
}

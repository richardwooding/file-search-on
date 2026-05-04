package celexpr_test

import (
	"reflect"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		// Classic single-edit cases.
		{"kitten", "sitting", 3},
		{"sunday", "saturday", 3},
		{"flaw", "lawn", 2},
		// Case-sensitive.
		{"Apple", "apple", 1},
		// Rune-aware (multi-byte runes count as one edit, not three).
		{"café", "cafe", 1},
		{"naïve", "naive", 1},
		// Asymmetric lengths trigger the "shorter side along columns" branch.
		{"a", "abcdef", 5},
		{"abcdef", "a", 5},
	}
	for _, tc := range cases {
		got := celexpr.Levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSoundex(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Knuth's worked examples (TAOCP §6.1).
		{"Robert", "R163"},
		{"Rupert", "R163"},
		{"Rubin", "R150"},
		{"Ashcraft", "A261"},
		{"Tymczak", "T522"},
		{"Pfister", "P236"},
		// Case insensitivity.
		{"smith", "S530"},
		{"SMITH", "S530"},
		// Vowels separate same-code consonants (e.g. C-A-S keeps both 2's? no
		// — vowel resets state so a same-code letter on the other side still
		// emits): Jackson -> J250 (J-K both code 2, but they're adjacent and
		// the K is not preserved; c collapses into the J). The historic
		// answer is "J250".
		{"Jackson", "J250"},
		// Pad short input to 4 chars.
		{"Lee", "L000"},
		// Truncate long output to 4 chars.
		{"Washington", "W252"},
		// Empty / no-letter input.
		{"", "0000"},
		{"123", "0000"},
		// Non-letter characters skipped.
		{"O'Brien", "O165"},
	}
	for _, tc := range cases {
		got := celexpr.Soundex(tc.in)
		if got != tc.want {
			t.Errorf("Soundex(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNgrams(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want []string
	}{
		{"hello", 2, []string{"he", "el", "ll", "lo"}},
		{"hello", 3, []string{"hel", "ell", "llo"}},
		{"abc", 3, []string{"abc"}},
		{"abc", 4, []string{}},        // n > len
		{"hello", 0, []string{}},      // n == 0
		{"hello", -1, []string{}},     // n negative
		{"", 2, []string{}},           // empty input
		{"café", 2, []string{"ca", "af", "fé"}}, // rune-aware (é is 1 rune)
	}
	for _, tc := range cases {
		got := celexpr.Ngrams(tc.s, tc.n)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Ngrams(%q, %d) = %v, want %v", tc.s, tc.n, got, tc.want)
		}
	}
}

func TestNgramSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		n    int
		want float64
	}{
		// Both empty by the n-gram set definition (both shorter than n).
		{"", "", 2, 1.0},
		// One empty, one not — totally dissimilar.
		{"", "hello", 2, 0.0},
		// Identical strings — full overlap.
		{"hello", "hello", 2, 1.0},
		// Single typo — most n-grams shared.
		{"kubernetes", "kubernetez", 3, 7.0 / 9.0},
		// Disjoint short strings — zero overlap.
		{"abc", "xyz", 2, 0.0},
	}
	const epsilon = 1e-9
	for _, tc := range cases {
		got := celexpr.NgramSimilarity(tc.a, tc.b, tc.n)
		if diff := got - tc.want; diff < -epsilon || diff > epsilon {
			t.Errorf("NgramSimilarity(%q, %q, %d) = %g, want %g", tc.a, tc.b, tc.n, got, tc.want)
		}
	}
}

func TestEvaluateFuzzyFunctions(t *testing.T) {
	// Use FileAttributes directly so test is independent of content-type
	// detection. The artist / camera_make / title fields ride along via Extra.
	attrs := &celexpr.FileAttributes{
		Name:        "song.mp3",
		Path:        "/music/song.mp3",
		Dir:         "/music",
		Size:        1024,
		Ext:         ".mp3",
		ContentType: "audio/mp3",
		IsAudio:     true,
		Extra: map[string]any{
			"artist":      "Radiohad",  // typo of "Radiohead"
			"camera_make": "NIKON",
			"title":       "kubernates", // typo of "kubernetes"
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{`levenshtein(artist, "Radiohead") <= 2`, true},
		{`levenshtein(artist, "Beethoven") <= 2`, false},
		{`soundex(camera_make) == soundex("Nikon")`, true},
		{`soundex(camera_make) == soundex("Sony")`, false},
		{`ngrams(title, 3).size() > 5`, true},
		{`ngram_similarity(title, "kubernetes", 2) > 0.6`, true},
		{`ngram_similarity(title, "docker", 2) < 0.3`, true},
	}
	for _, tc := range cases {
		eval, err := celexpr.New(tc.expr)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.expr, err)
		}
		got, err := eval.Evaluate(attrs)
		if err != nil {
			t.Fatalf("eval %q: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("expr %q: got %v, want %v", tc.expr, got, tc.want)
		}
	}
}

package search

import "slices"

import "testing"

func TestSkipPrefixesForProfile(t *testing.T) {
	if got := skipPrefixesForProfile(""); got != nil {
		t.Errorf("empty profile should return nil; got %v", got)
	}
	if got := skipPrefixesForProfile("unknown"); got != nil {
		t.Errorf("unknown profile should return nil; got %v", got)
	}
	code := skipPrefixesForProfile("code")
	if len(code) == 0 {
		t.Fatal("code profile should return a non-empty skip list")
	}
	// Spot-check expected families.
	for _, want := range []string{"image/", "audio/", "video/", "binary/", "archive/"} {
		found := slices.Contains(code, want)
		if !found {
			t.Errorf("code skip list missing %q; got %v", want, code)
		}
	}
}

func TestMatchesSkipPrefix(t *testing.T) {
	prefixes := []string{"image/", "audio/", "video/"}
	cases := map[string]bool{
		"image/jpeg": true,
		"video/mp4":  true,
		"audio/mpeg": true,
		"source/go":  false,
		"markdown":   false,
		"":           false,
		"image":      false, // no slash → not a prefix match for "image/"
	}
	for ct, want := range cases {
		if got := matchesSkipPrefix(ct, prefixes); got != want {
			t.Errorf("matchesSkipPrefix(%q) = %v, want %v", ct, got, want)
		}
	}
	// Empty prefix list never skips.
	if matchesSkipPrefix("image/jpeg", nil) {
		t.Error("nil prefixes should never skip")
	}
}

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

package content_test

import (
	"bytes"
	"context"
	"io/fs"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// magicMatcher mirrors the unexported content.MagicMatcher interface so
// the test can tell which registered types disambiguate structurally.
type magicMatcher interface {
	MatchMagic(head []byte) bool
}

// magicConflicts returns a human-readable conflict for every pair of
// types whose MagicBytes prefixes overlap (one is a prefix of the other,
// so a single input could HasPrefix-match both) where at least one type
// lacks a MagicMatcher — i.e. detection would be registration-order-
// dependent. Issue #334.
func magicConflicts(types []content.ContentType) []string {
	overlaps := func(a, b [][]byte) bool {
		for _, x := range a {
			for _, y := range b {
				if len(x) == 0 || len(y) == 0 {
					continue
				}
				if bytes.HasPrefix(x, y) || bytes.HasPrefix(y, x) {
					return true
				}
			}
		}
		return false
	}
	var out []string
	for i := range types {
		for j := i + 1; j < len(types); j++ {
			a, b := types[i], types[j]
			if len(a.MagicBytes()) == 0 || len(b.MagicBytes()) == 0 {
				continue // extension-only types don't magic-sniff
			}
			if !overlaps(a.MagicBytes(), b.MagicBytes()) {
				continue
			}
			_, aOK := a.(magicMatcher)
			_, bOK := b.(magicMatcher)
			if !aOK || !bOK {
				out = append(out, a.Name()+" / "+b.Name())
			}
		}
	}
	return out
}

// TestMagicConflicts is the regression guard: the real registry must
// have NO order-dependent magic conflicts. Catches the #322 class
// (RIFF: WebP/AVI/WAV) if a future type claims an overlapping magic
// without a MagicMatcher.
func TestMagicConflicts(t *testing.T) {
	if c := magicConflicts(content.DefaultRegistry().Types()); len(c) > 0 {
		t.Errorf("registration-order-dependent magic conflicts (add a MagicMatcher to disambiguate, issue #334):\n  %v", c)
	}
}

// fakeType is a minimal ContentType for exercising magicConflicts.
type fakeType struct {
	name  string
	magic [][]byte
}

func (f fakeType) Name() string         { return f.name }
func (f fakeType) Extensions() []string { return nil }
func (f fakeType) MagicBytes() [][]byte { return f.magic }
func (f fakeType) Attributes(context.Context, fs.FS, string) (content.Attributes, error) {
	return nil, nil
}

type fakeMatcherType struct{ fakeType }

func (f fakeMatcherType) MatchMagic([]byte) bool { return true }

// TestMagicConflicts_Detects proves the guard fires: two "RIFF" types
// where one lacks a MagicMatcher is a conflict; once both implement it,
// it's clean.
func TestMagicConflicts_Detects(t *testing.T) {
	riff := [][]byte{[]byte("RIFF")}
	unguarded := []content.ContentType{
		fakeType{name: "a/webp", magic: riff},
		fakeType{name: "a/wav", magic: riff},
	}
	if c := magicConflicts(unguarded); len(c) != 1 {
		t.Errorf("want 1 conflict for two unguarded RIFF types, got %v", c)
	}
	guarded := []content.ContentType{
		fakeMatcherType{fakeType{name: "a/webp", magic: riff}},
		fakeMatcherType{fakeType{name: "a/wav", magic: riff}},
	}
	if c := magicConflicts(guarded); len(c) != 0 {
		t.Errorf("two guarded RIFF types must not conflict, got %v", c)
	}
}

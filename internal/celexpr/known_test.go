package celexpr_test

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	hashset "github.com/richardwooding/go-hashset"
)

// TestBuildAttributesWith_KnownGood populates IsKnownGood when the
// file's MD5 appears in the allowlist.
func TestBuildAttributesWith_KnownGood(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	body := []byte("known content\n")
	mustWrite(t, path, string(body))

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	md5sum := md5.Sum(body)
	md5hex := hex.EncodeToString(md5sum[:])
	allow, err := hashset.LoadText(strings.NewReader(md5hex + "\n"))
	if err != nil {
		t.Fatalf("LoadText: %v", err)
	}
	defer func() { _ = allow.Close() }()

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		ComputeHashes: true,
		Allowlist:     allow,
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if !a.IsKnownGood {
		t.Errorf("IsKnownGood=false; MD5=%q allowlist counts=%+v", a.MD5, allow.Counts())
	}
	if a.IsKnownBad {
		t.Errorf("IsKnownBad=true on allowlist-only setup")
	}
}

// TestBuildAttributesWith_KnownBad populates IsKnownBad via SHA1.
// Exercises the "any algorithm match" rule: only SHA1 is in the
// denylist; the file's SHA1 is computed alongside MD5/SHA256 so the
// lookup hits.
func TestBuildAttributesWith_KnownBad(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "evil.bin")
	body := []byte("threat content\n")
	mustWrite(t, path, string(body))

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	sha1sum := sha1.Sum(body)
	sha1hex := hex.EncodeToString(sha1sum[:])
	deny, err := hashset.LoadText(strings.NewReader(sha1hex + "\n"))
	if err != nil {
		t.Fatalf("LoadText: %v", err)
	}
	defer func() { _ = deny.Close() }()

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		ComputeHashes: true,
		Denylist:      deny,
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if !a.IsKnownBad {
		t.Errorf("IsKnownBad=false; SHA1=%q denylist counts=%+v", a.SHA1, deny.Counts())
	}
}

// TestBuildAttributesWith_Unknown: a file whose hashes aren't in
// either list fires NEITHER predicate.
func TestBuildAttributesWith_Unknown(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	mustWrite(t, path, "unique content nobody indexed\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Allowlist + denylist with unrelated hashes.
	other := sha256.Sum256([]byte("not this file"))
	otherHex := hex.EncodeToString(other[:])
	allow, _ := hashset.LoadText(strings.NewReader(otherHex + "\n"))
	deny, _ := hashset.LoadText(strings.NewReader(otherHex + "\n"))
	defer func() { _ = allow.Close() }()
	defer func() { _ = deny.Close() }()

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		ComputeHashes: true,
		Allowlist:     allow,
		Denylist:      deny,
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.IsKnownGood || a.IsKnownBad {
		t.Errorf("expected neither flag; got good=%v bad=%v",
			a.IsKnownGood, a.IsKnownBad)
	}
}

// TestEvaluate_KnownFilters exercises the CEL predicates from
// compiled expressions.
func TestEvaluate_KnownFilters(t *testing.T) {
	good := &celexpr.FileAttributes{IsKnownGood: true}
	bad := &celexpr.FileAttributes{IsKnownBad: true}
	neither := &celexpr.FileAttributes{}

	ev, err := celexpr.New(`is_known_good`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if m, _ := ev.Evaluate(good); !m {
		t.Errorf("is_known_good should match")
	}
	if m, _ := ev.Evaluate(bad); m {
		t.Errorf("is_known_good should NOT match bad-only file")
	}

	notGood, err := celexpr.New(`!is_known_good`)
	if err != nil {
		t.Fatalf("compile !good: %v", err)
	}
	if m, _ := notGood.Evaluate(neither); !m {
		t.Errorf("!is_known_good should match a file with neither flag")
	}

	ev2, err := celexpr.New(`is_known_bad`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if m, _ := ev2.Evaluate(bad); !m {
		t.Errorf("is_known_bad should match")
	}
}

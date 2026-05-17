package celexpr_test

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// TestBuildAttributesWith_ComputeHashes_OffByDefault confirms that
// without the ComputeHashes flag the trio stays empty — the walker
// pays no hashing cost.
func TestBuildAttributesWith_ComputeHashes_OffByDefault(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	mustWrite(t, path, "hello world\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.MD5 != "" || a.SHA1 != "" || a.SHA256 != "" {
		t.Errorf("expected empty hashes without ComputeHashes, got md5=%q sha1=%q sha256=%q", a.MD5, a.SHA1, a.SHA256)
	}
}

// TestBuildAttributesWith_ComputeHashes_Populates confirms that
// ComputeHashes=true fills md5, sha1, sha256 with the expected
// canonical hex of the file's bytes.
func TestBuildAttributesWith_ComputeHashes_Populates(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	body := []byte("hello world\n")
	mustWrite(t, path, string(body))

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{ComputeHashes: true})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	md5Sum := md5.Sum(body)
	sha1Sum := sha1.Sum(body)
	sha256Sum := sha256.Sum256(body)
	wantMD5 := hex.EncodeToString(md5Sum[:])
	wantSHA1 := hex.EncodeToString(sha1Sum[:])
	wantSHA256 := hex.EncodeToString(sha256Sum[:])

	if a.MD5 != wantMD5 {
		t.Errorf("md5=%q want %q", a.MD5, wantMD5)
	}
	if a.SHA1 != wantSHA1 {
		t.Errorf("sha1=%q want %q", a.SHA1, wantSHA1)
	}
	if a.SHA256 != wantSHA256 {
		t.Errorf("sha256=%q want %q", a.SHA256, wantSHA256)
	}
}

// TestBuildAttributesWith_ComputeHashes_CacheRoundTrip confirms the
// hash trio caches: cold call hashes + stores; warm call hits the
// index (no file read needed), still surfaces the same three.
func TestBuildAttributesWith_ComputeHashes_CacheRoundTrip(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	mustWrite(t, path, "cache me\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	opts := celexpr.BuildOptions{Index: idx, ComputeHashes: true}

	a1, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("cold: %v", err)
	}
	if a1.SHA256 == "" {
		t.Fatalf("expected SHA256 populated cold; attrs=%+v", a1)
	}

	a2, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("warm: %v", err)
	}
	if a2.SHA256 != a1.SHA256 || a2.MD5 != a1.MD5 || a2.SHA1 != a1.SHA1 {
		t.Errorf("warm trio drift: cold=(%q,%q,%q) warm=(%q,%q,%q)",
			a1.MD5, a1.SHA1, a1.SHA256, a2.MD5, a2.SHA1, a2.SHA256)
	}

	st := idx.Stats()
	if st.Hits == 0 {
		t.Errorf("expected attribute cache Hits >= 1 on warm; stats=%+v", st)
	}
}

// TestEvaluate_HashFilters confirms md5 / sha1 / sha256 are
// addressable from CEL — `md5 == "<known>"` etc. evaluate correctly.
func TestEvaluate_HashFilters(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	body := []byte("forensic content\n")
	mustWrite(t, path, string(body))

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{ComputeHashes: true})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	md5Sum := md5.Sum(body)
	wantMD5 := hex.EncodeToString(md5Sum[:])

	ev, err := celexpr.New(`md5 == "` + wantMD5 + `"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	match, err := ev.Evaluate(a)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !match {
		t.Errorf("expected md5 filter to match; got false")
	}

	ev2, err := celexpr.New(`md5 == "deadbeef"`)
	if err != nil {
		t.Fatalf("compile2: %v", err)
	}
	match, err = ev2.Evaluate(a)
	if err != nil {
		t.Fatalf("evaluate2: %v", err)
	}
	if match {
		t.Errorf("expected sentinel hash to NOT match; got true")
	}
}


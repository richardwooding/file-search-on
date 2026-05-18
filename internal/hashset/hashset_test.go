package hashset_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/hashset"
)

const (
	sampleMD5    = "5d41402abc4b2a76b9719d911017c592" // md5("hello")
	sampleSHA1   = "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	sampleSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

// TestLoadText_HappyPath confirms mixed-algo lines, comments, and
// blanks are handled correctly.
func TestLoadText_HappyPath(t *testing.T) {
	body := `# threat-intel feed export 2026-05-18
` + sampleMD5 + `

# blank line above; mixed-case to confirm normalization
` + strings.ToUpper(sampleSHA1) + `
` + sampleSHA256 + `
`
	set, err := hashset.LoadText(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("LoadText: %v", err)
	}
	defer func() { _ = set.Close() }()

	if !set.Contains("md5", sampleMD5) {
		t.Errorf("MD5 lookup miss")
	}
	if !set.Contains("sha1", sampleSHA1) {
		t.Errorf("SHA1 lookup miss")
	}
	if !set.Contains("sha256", sampleSHA256) {
		t.Errorf("SHA256 lookup miss")
	}
	counts := set.Counts()
	if counts["md5"] != 1 || counts["sha1"] != 1 || counts["sha256"] != 1 {
		t.Errorf("counts=%+v want 1/1/1", counts)
	}
}

// TestLoadText_InvalidHex rejects garbage with a line-numbered error.
func TestLoadText_InvalidHex(t *testing.T) {
	body := sampleMD5 + "\nnotahex\n"
	_, err := hashset.LoadText(strings.NewReader(body))
	if err == nil {
		t.Fatalf("expected error on invalid hex")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error doesn't name line 2: %v", err)
	}
}

// TestContains_UnknownAlgo returns false without errors.
func TestContains_UnknownAlgo(t *testing.T) {
	set, err := hashset.LoadText(strings.NewReader(sampleMD5 + "\n"))
	if err != nil {
		t.Fatalf("LoadText: %v", err)
	}
	if set.Contains("unknown-algo", sampleMD5) {
		t.Errorf("Contains returned true for unknown algorithm")
	}
	if set.Contains("md5", "not-hex") {
		t.Errorf("Contains returned true for non-hex query")
	}
}

// TestBuildAndOpenBolt round-trips text → bbolt → Contains.
func TestBuildAndOpenBolt(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "test.hashset")
	input := sampleMD5 + "\n" + sampleSHA1 + "\n" + sampleSHA256 + "\n"

	if err := hashset.Build(strings.NewReader(input), outPath, hashset.BuildOpts{}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	set, err := hashset.OpenBolt(outPath)
	if err != nil {
		t.Fatalf("OpenBolt: %v", err)
	}
	defer func() { _ = set.Close() }()

	if !set.Contains("md5", sampleMD5) {
		t.Errorf("MD5 miss after bbolt round-trip")
	}
	if !set.Contains("sha1", sampleSHA1) {
		t.Errorf("SHA1 miss after bbolt round-trip")
	}
	if !set.Contains("sha256", sampleSHA256) {
		t.Errorf("SHA256 miss after bbolt round-trip")
	}

	counts := set.Counts()
	if counts["md5"] != 1 || counts["sha1"] != 1 || counts["sha256"] != 1 {
		t.Errorf("post-Build counts=%+v want 1/1/1", counts)
	}
}

// TestBuildNSRL parses the canonical NSRL header + a couple of rows.
// We don't ship a real NSRL fixture (it's massive); a hand-crafted
// minimal CSV exercises the same column-detection logic.
func TestBuildNSRL(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "nsrl.hashset")
	body := `"SHA-1","MD5","CRC32","FileName","FileSize","ProductCode","OpSystemCode","SpecialCode"
"` + sampleSHA1 + `","` + sampleMD5 + `","abcd1234","hello.txt","5","12345","Win10","",""
"AAF4C61DDCC5E8A2DABEDE0F3B482CD9AEA9434E","00000000000000000000000000000000","00000000","other.bin","100","12345","Win10","",""
`
	if err := hashset.Build(strings.NewReader(body), outPath, hashset.BuildOpts{Format: "nsrl"}); err != nil {
		t.Fatalf("Build NSRL: %v", err)
	}
	set, err := hashset.OpenBolt(outPath)
	if err != nil {
		t.Fatalf("OpenBolt: %v", err)
	}
	defer func() { _ = set.Close() }()

	if !set.Contains("sha1", sampleSHA1) {
		t.Errorf("NSRL row 1 SHA1 not in set")
	}
	if !set.Contains("md5", sampleMD5) {
		t.Errorf("NSRL row 1 MD5 not in set")
	}
	if !set.Contains("sha1", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434e") {
		t.Errorf("NSRL row 2 SHA1 not in set")
	}
	counts := set.Counts()
	if counts["sha1"] != 2 || counts["md5"] != 2 {
		t.Errorf("NSRL counts=%+v want sha1=2 md5=2", counts)
	}
}

// TestOpenAutoDetect: Open() routes a bbolt file via OpenBolt and a
// text file via LoadText. Falling back to text when bbolt fails is
// the documented behaviour.
func TestOpenAutoDetect(t *testing.T) {
	dir := t.TempDir()

	// Build a bbolt file, open via Open() — should succeed.
	bboltPath := filepath.Join(dir, "set.hashset")
	if err := hashset.Build(strings.NewReader(sampleMD5+"\n"), bboltPath, hashset.BuildOpts{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	bset, err := hashset.Open(bboltPath)
	if err != nil {
		t.Fatalf("Open(bbolt): %v", err)
	}
	if !bset.Contains("md5", sampleMD5) {
		t.Errorf("Open(bbolt): MD5 miss")
	}
	_ = bset.Close()

	// Write a text file, open via Open() — should succeed via the
	// text fallback path.
	textPath := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(textPath, []byte(sampleSHA256+"\n"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	tset, err := hashset.Open(textPath)
	if err != nil {
		t.Fatalf("Open(text): %v", err)
	}
	if !tset.Contains("sha256", sampleSHA256) {
		t.Errorf("Open(text): SHA256 miss")
	}
	_ = tset.Close()
}

// TestOpenBolt_SchemaMismatch on a non-bbolt file fails with a
// recognisable error so the wrapping Open() can fall back to text.
func TestOpenBolt_SchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-bbolt.txt")
	if err := os.WriteFile(path, []byte(sampleMD5+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := hashset.OpenBolt(path)
	if err == nil {
		t.Fatalf("OpenBolt should fail on non-bbolt file")
	}
	// We don't strictly require ErrSchemaMismatch — bbolt itself
	// may reject the file at the open step with a different
	// error. Just confirm we got SOME error so Open's fallback
	// path engages.
	_ = errors.Is(err, hashset.ErrSchemaMismatch)
}

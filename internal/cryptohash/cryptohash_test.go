package cryptohash_test

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/cryptohash"
)

// TestFile_Trio confirms md5/sha1/sha256 all compute correctly in
// one pass against a known-content file.
func TestFile_Trio(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	body := []byte("forensic content\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := cryptohash.File(t.Context(), path)
	if err != nil {
		t.Fatalf("File: %v", err)
	}

	md5Sum := md5.Sum(body)
	sha1Sum := sha1.Sum(body)
	sha256Sum := sha256.Sum256(body)
	if got.MD5 != hex.EncodeToString(md5Sum[:]) {
		t.Errorf("md5=%q", got.MD5)
	}
	if got.SHA1 != hex.EncodeToString(sha1Sum[:]) {
		t.Errorf("sha1=%q", got.SHA1)
	}
	if got.SHA256 != hex.EncodeToString(sha256Sum[:]) {
		t.Errorf("sha256=%q", got.SHA256)
	}
}

// TestFile_Empty exercises the zero-byte boundary.
func TestFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := cryptohash.File(t.Context(), path)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	// Empty-file canonical hashes.
	if got.MD5 != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("empty md5=%q", got.MD5)
	}
	if got.SHA1 != "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Errorf("empty sha1=%q", got.SHA1)
	}
	if got.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("empty sha256=%q", got.SHA256)
	}
}

// TestFile_MissingPath confirms a non-existent path returns an error
// rather than panicking.
func TestFile_MissingPath(t *testing.T) {
	_, err := cryptohash.File(t.Context(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Errorf("expected error for missing path")
	}
}

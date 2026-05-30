package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestAttrsCmd_PopulatesIndex confirms the long-standing gap is
// closed: running `attrs` on a file with an explicit --index-path
// stores the attribute entry, so a subsequent walking subcommand
// would see a cache hit on that path.
func TestAttrsCmd_PopulatesIndex(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fixture.md")
	if err := os.WriteFile(target, []byte("# title\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	dbPath := filepath.Join(tmp, "attrs.db")

	cmd := &AttrsCmd{Path: target, Output: "json", IndexPath: dbPath}
	if _, err := captureStdout(t, func() error { return cmd.Run(t.Context()) }); err != nil {
		t.Fatalf("attrs run: %v", err)
	}

	// Re-open the index and confirm the file's entry made it in.
	idx, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("re-open index: %v", err)
	}
	defer func() { _ = idx.Close() }()

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	if _, ok := idx.Lookup(target, info.Size(), info.ModTime()); !ok {
		t.Fatalf("attrs did not populate the index for %s (expected cache hit on lookup)", target)
	}
}

// TestAttrsCmd_NoIndex_LeavesNoFile confirms --no-index skips the
// disk entirely — no bbolt file appears at the explicit path.
func TestAttrsCmd_NoIndex_LeavesNoFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fixture.md")
	if err := os.WriteFile(target, []byte("# title\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	dbPath := filepath.Join(tmp, "should-not-be-created.db")

	cmd := &AttrsCmd{Path: target, Output: "json", IndexPath: dbPath, NoIndex: true}
	if _, err := captureStdout(t, func() error { return cmd.Run(t.Context()) }); err != nil {
		t.Fatalf("attrs run: %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("--no-index should not create %s (stat err = %v)", dbPath, err)
	}
}

// TestAttrsCmd_WithHashes confirms md5 / sha1 / sha256 land on the
// JSON output when --with-hashes is set, matching the read_attributes
// MCP tool's compute_hashes=true behaviour.
func TestAttrsCmd_WithHashes(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "fixture.txt")
	// Single known string → deterministic hashes (sha256 of "hello\n"
	// is well-known, so the test could even pin the digest, but
	// presence-only is enough to confirm the wire-through).
	if err := os.WriteFile(target, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := &AttrsCmd{
		Path:       target,
		Output:     "json",
		NoIndex:    true, // hermetic — no disk side effect from the cache
		WithHashes: true,
	}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("attrs run: %v", err)
	}

	// attrs emits a single JSON object (not an array — one file = one
	// record). Decode straight into a map.
	var r map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&r); err != nil {
		t.Fatalf("decode JSON output: %v\nraw: %q", err, out)
	}
	for _, k := range []string{"md5", "sha1", "sha256"} {
		v, ok := r[k]
		if !ok {
			t.Errorf("missing %s in --with-hashes output", k)
			continue
		}
		s, _ := v.(string)
		if s == "" {
			t.Errorf("%s is empty; expected lowercase hex digest", k)
		}
	}
	// sha256 of "hello\n" is a well-known constant — pin it as a
	// regression guard.
	const wantSHA256 = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"
	if got, _ := r["sha256"].(string); got != wantSHA256 {
		t.Errorf("sha256 = %q, want %q", got, wantSHA256)
	}
}

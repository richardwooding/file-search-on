package search_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestFindMatches_TruncationSurfaced confirms the #283 contract:
// when a file has a single line longer than the scanner's per-line
// buffer cap (64 KiB), the file path lands in TruncatedFiles so the
// caller knows the scan was incomplete. A regex match HIDDEN past
// the cap silently misses (the documented limitation that motivated
// the warning).
func TestFindMatches_TruncationSurfaced(t *testing.T) {
	dir := t.TempDir()

	// Hand-craft a file with two lines:
	//   line 1: 70 KiB of filler — exceeds the 64 KiB scanner cap.
	//   line 2: a real "TODO" annotation that should be findable.
	// Because line 1 trips bufio.ErrTooLong, line 2 never reaches the
	// scanner and the TODO match is missed.
	longLine := strings.Repeat("x", 70*1024)
	body := longLine + "\n// TODO real annotation\n"
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A second normal file with a real TODO — should match cleanly.
	normal := "// TODO another one\n"
	if err := os.WriteFile(filepath.Join(dir, "normal.go"), []byte(normal), 0o644); err != nil {
		t.Fatalf("write normal: %v", err)
	}

	res, err := search.FindMatches(context.Background(), search.Options{
		Root:    dir,
		Expr:    `is_source && language == "go"`,
		Pattern: "TODO",
		Workers: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}

	// big.go's TODO sat past the truncated line → not matched.
	// normal.go's TODO matched cleanly → exactly one result.
	if res.Count != 1 {
		t.Errorf("Count = %d, want 1 (big.go's TODO is past the cap; only normal.go matched)", res.Count)
	}
	// TruncatedFiles must include big.go.
	gotTruncated := false
	for _, p := range res.TruncatedFiles {
		if strings.HasSuffix(p, "big.go") {
			gotTruncated = true
			break
		}
	}
	if !gotTruncated {
		t.Errorf("TruncatedFiles should include big.go; got %v", res.TruncatedFiles)
	}
	// normal.go MUST NOT appear in TruncatedFiles.
	for _, p := range res.TruncatedFiles {
		if strings.HasSuffix(p, "normal.go") {
			t.Errorf("normal.go should NOT be in TruncatedFiles: %v", res.TruncatedFiles)
		}
	}
}

// TestFindMatches_NoTruncationWhenLinesFit confirms TruncatedFiles
// stays empty on well-behaved input.
func TestFindMatches_NoTruncationWhenLinesFit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.go"), []byte("// TODO tiny\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := search.FindMatches(context.Background(), search.Options{
		Root:    dir,
		Pattern: "TODO",
		Workers: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(res.TruncatedFiles) != 0 {
		t.Errorf("TruncatedFiles should be empty on well-behaved input; got %v", res.TruncatedFiles)
	}
}

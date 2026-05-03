package content_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestMaxLineBytesDefault(t *testing.T) {
	// SetMaxLineBytes was called by other tests; reset to default for a clean check.
	content.SetMaxLineBytes(content.DefaultMaxLineBytes)
	if got := content.MaxLineBytes(); got != content.DefaultMaxLineBytes {
		t.Fatalf("MaxLineBytes() = %d, want %d", got, content.DefaultMaxLineBytes)
	}
}

func TestSetMaxLineBytesIgnoresZeroAndNegative(t *testing.T) {
	content.SetMaxLineBytes(content.DefaultMaxLineBytes)
	prev := content.MaxLineBytes()

	content.SetMaxLineBytes(0)
	if got := content.MaxLineBytes(); got != prev {
		t.Errorf("after SetMaxLineBytes(0): got %d, want %d unchanged", got, prev)
	}

	content.SetMaxLineBytes(-1)
	if got := content.MaxLineBytes(); got != prev {
		t.Errorf("after SetMaxLineBytes(-1): got %d, want %d unchanged", got, prev)
	}

	t.Cleanup(func() {
		content.SetMaxLineBytes(content.DefaultMaxLineBytes)
	})
}

// TestTextRespectsLineCap proves the knob actually changes scanner behaviour:
// a single line longer than the cap fails the scan, leaving line_count at 0.
// Raising the cap above the line length recovers the count.
func TestTextRespectsLineCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	body := strings.Repeat("x", 200_000) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		content.SetMaxLineBytes(content.DefaultMaxLineBytes)
	})

	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "text" {
		t.Fatalf("Detect: got %v, want text", ct)
	}

	// 64 KiB cap: scanner can't fit the 200 KB line, returns no records.
	content.SetMaxLineBytes(64 * 1024)
	attrs, err := ct.Attributes(path)
	if err != nil {
		t.Fatalf("Attributes (low cap): %v", err)
	}
	if got := attrs["line_count"]; got != int64(0) {
		t.Errorf("line_count with low cap = %v, want 0 (line truncated by scanner)", got)
	}

	// 1 MiB cap: line fits, count is 1.
	content.SetMaxLineBytes(content.DefaultMaxLineBytes)
	attrs, err = ct.Attributes(path)
	if err != nil {
		t.Fatalf("Attributes (default cap): %v", err)
	}
	if got := attrs["line_count"]; got != int64(1) {
		t.Errorf("line_count with default cap = %v, want 1", got)
	}
}

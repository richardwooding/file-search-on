package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestTextAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	body := "first line has five words\nsecond line\nthird\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "text" {
		t.Fatalf("Detect: got %v, want text", ct)
	}

	attrs, err := ct.Attributes(path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["line_count"]; got != int64(3) {
		t.Errorf("line_count = %v, want 3", got)
	}
	if got := attrs["word_count"]; got != int64(8) {
		t.Errorf("word_count = %v, want 8", got)
	}
}

func TestTextDetectionByExtension(t *testing.T) {
	dir := t.TempDir()
	for _, ext := range []string{".txt", ".text", ".log"} {
		path := filepath.Join(dir, "f"+ext)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ct := content.DefaultRegistry().Detect(path)
		if ct == nil || ct.Name() != "text" {
			t.Errorf("Detect(%s): got %v, want text", ext, ct)
		}
	}
}

func TestTextEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	attrs, err := ct.Attributes(path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["line_count"]; got != int64(0) {
		t.Errorf("line_count = %v, want 0", got)
	}
	if got := attrs["word_count"]; got != int64(0) {
		t.Errorf("word_count = %v, want 0", got)
	}
}

package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func detectBothAt(path string) (nameType, magicType content.ContentType) {
	dir := filepath.Dir(path)
	return content.DefaultRegistry().DetectBoth(os.DirFS(dir), filepath.Base(path))
}

// TestDetectBoth_AgreeingFile confirms that an honest file (extension
// matches magic) produces two non-nil results with equal Name().
func TestDetectBoth_AgreeingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := os.WriteFile(path, []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	name, magic := detectBothAt(path)
	if name == nil || name.Name() != "json" {
		t.Errorf("nameType=%v want json", name)
	}
	if magic == nil || magic.Name() != "json" {
		t.Errorf("magicType=%v want json", magic)
	}
}

// TestDetectBoth_Disguised forces the classic mismatch: PE-magic
// bytes in a .txt file. nameType resolves to text (from the
// extension), magicType resolves to binary/pe (from the bytes).
func TestDetectBoth_Disguised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(path, []byte("MZ\x90\x00\x03\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, magic := detectBothAt(path)
	if name == nil || name.Name() != "text" {
		t.Errorf("nameType=%v want text", name)
	}
	if magic == nil || magic.Name() != "binary/pe" {
		t.Errorf("magicType=%v want binary/pe", magic)
	}
	if name != nil && magic != nil && name.Name() == magic.Name() {
		t.Errorf("expected disagreement; both = %s", name.Name())
	}
}

// TestDetectBoth_MagicOnly: an extensionless file with magic bytes
// reports nameType=nil, magicType=<the format>. Important for the
// is_disguised predicate: extension being nil means we DON'T fire
// (no extension to contradict).
func TestDetectBoth_MagicOnly(t *testing.T) {
	dir := t.TempDir()
	// JSON magic '{' but no extension.
	path := filepath.Join(dir, "noext")
	if err := os.WriteFile(path, []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	name, magic := detectBothAt(path)
	if name != nil {
		t.Errorf("nameType=%v want nil (extensionless)", name)
	}
	if magic == nil || magic.Name() != "json" {
		t.Errorf("magicType=%v want json", magic)
	}
}

// TestDetectBoth_ExtensionOnly: a file with a recognised extension
// but no magic-matching bytes (e.g. .md plain text) returns
// nameType=markdown, magicType=nil.
func TestDetectBoth_ExtensionOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("# Heading\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, magic := detectBothAt(path)
	if name == nil || name.Name() != "markdown" {
		t.Errorf("nameType=%v want markdown", name)
	}
	if magic != nil {
		t.Errorf("magicType=%v want nil (no magic for markdown)", magic)
	}
}

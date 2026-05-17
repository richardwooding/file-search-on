package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestBuildAttributesWith_Disguised_OffByDefault confirms that the
// three disguise attributes stay empty / false when CheckDisguised
// isn't set — no extra file read, no surprise filtering.
func TestBuildAttributesWith_Disguised_OffByDefault(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "# h\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.MagicContentType != "" || a.ExtensionContentType != "" || a.IsDisguised {
		t.Errorf("disguise attrs populated without opt-in: magic=%q ext=%q disg=%v",
			a.MagicContentType, a.ExtensionContentType, a.IsDisguised)
	}
}

// TestBuildAttributesWith_Disguised_Detects forces the classic
// "extension lies" scenario: write PE-magic bytes into a file named
// .txt. The bytes are read as PE; the extension says text; the
// predicate fires.
func TestBuildAttributesWith_Disguised_Detects(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	// "MZ" — the universal PE/EXE magic. The remaining bytes need
	// to make registry.DetectBoth happy; everything past the magic
	// match is irrelevant to the prefix check.
	mustWrite(t, path, "MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{CheckDisguised: true})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.ExtensionContentType != "text" {
		t.Errorf("ExtensionContentType=%q want text", a.ExtensionContentType)
	}
	if a.MagicContentType != "binary/pe" {
		t.Errorf("MagicContentType=%q want binary/pe", a.MagicContentType)
	}
	if !a.IsDisguised {
		t.Errorf("IsDisguised=false; magic=%q ext=%q", a.MagicContentType, a.ExtensionContentType)
	}
}

// TestBuildAttributesWith_Disguised_HonestFile confirms a file
// whose magic + extension agree does NOT fire the predicate.
func TestBuildAttributesWith_Disguised_HonestFile(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	mustWrite(t, path, `{"k":"v"}`)

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{CheckDisguised: true})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.IsDisguised {
		t.Errorf("IsDisguised fired on honest JSON file: magic=%q ext=%q",
			a.MagicContentType, a.ExtensionContentType)
	}
}

// TestEvaluate_IsDisguisedFilter exercises the predicate from CEL
// using a synthesised FileAttributes.
func TestEvaluate_IsDisguisedFilter(t *testing.T) {
	ev, err := celexpr.New(`is_disguised && magic_content_type.startsWith("binary/")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Disguised binary disguised as text.
	planted := &celexpr.FileAttributes{
		MagicContentType:     "binary/pe",
		ExtensionContentType: "text",
		IsDisguised:          true,
	}
	if match, err := ev.Evaluate(planted); err != nil || !match {
		t.Errorf("planted: match=%v err=%v", match, err)
	}

	// Disguised but not into a binary (e.g. JSON-in-txt). Predicate
	// must not fire.
	jsonInTxt := &celexpr.FileAttributes{
		MagicContentType:     "json",
		ExtensionContentType: "text",
		IsDisguised:          true,
	}
	if match, err := ev.Evaluate(jsonInTxt); err != nil || match {
		t.Errorf("jsonInTxt: match=%v err=%v want false", match, err)
	}

	// Not disguised at all.
	honest := &celexpr.FileAttributes{
		MagicContentType:     "json",
		ExtensionContentType: "json",
		IsDisguised:          false,
	}
	if match, err := ev.Evaluate(honest); err != nil || match {
		t.Errorf("honest: match=%v err=%v want false", match, err)
	}
}

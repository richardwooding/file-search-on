package search_test

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// writePNG writes a minimal valid 1×1 PNG to disk so the image
// detector picks it up by extension AND magic bytes. We can then
// observe whether image/* per-format attributes (width / height /
// img_format) ran or were skipped under profile=code.
func writePNG(t *testing.T, path string) {
	t.Helper()
	// 8-byte PNG signature.
	header := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	// IHDR chunk: 13 bytes payload (W4 H4 bd ct cm fm il).
	ihdr := make([]byte, 4+4+4+13+4)
	binary.BigEndian.PutUint32(ihdr[0:4], 13) // length
	copy(ihdr[4:8], "IHDR")
	binary.BigEndian.PutUint32(ihdr[8:12], 1)   // width
	binary.BigEndian.PutUint32(ihdr[12:16], 1)  // height
	ihdr[16] = 8                                 // bit depth
	ihdr[17] = 2                                 // colour type RGB
	// crc bytes left zero — image_png parser only needs width/height
	// from the IHDR header to populate img_width / img_height.
	out := append(header, ihdr...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

// TestWalk_ProfileCode_SkipsImageAttributes is the headline #284
// check: with profile=code, an image file is detected (content_type
// + is_image family flag fire) but the per-format img_width /
// img_height etc. are NOT populated. A sibling .go file is unaffected.
func TestWalk_ProfileCode_SkipsImageAttributes(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "pixel.png"))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go: %v", err)
	}

	res, err := search.Walk(context.Background(), search.Options{
		Root:              dir,
		Expr:              "true",
		Workers:           1,
		Profile:           "code",
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	var img, src *search.Result
	for i, r := range res {
		switch filepath.Base(r.Path) {
		case "pixel.png":
			img = &res[i]
		case "main.go":
			src = &res[i]
		}
	}
	if img == nil {
		t.Fatal("pixel.png not in results")
	}
	if src == nil {
		t.Fatal("main.go not in results")
	}

	// Detection still ran for the image — ContentType + is_image
	// family flag populate.
	if !strings.HasPrefix(img.ContentType, "image/") {
		t.Errorf("png content type = %q, want image/*", img.ContentType)
	}
	if img.Attrs == nil || !img.Attrs.IsImage {
		t.Errorf("pixel.png should still have is_image=true (family flag derived from content type)")
	}
	// Per-format image attributes were SKIPPED — img_width should
	// not have been populated.
	if img.Attrs != nil && img.Attrs.Extra != nil {
		if _, ok := img.Attrs.Extra["img_width"]; ok {
			t.Errorf("profile=code should skip image per-format parse; img_width should NOT be populated, got Extra=%v", img.Attrs.Extra)
		}
	}

	// The .go file is part of the keep-set — its language attribute
	// should still populate.
	if src.Attrs == nil || src.Attrs.Extra == nil {
		t.Fatal("main.go's Attrs.Extra is unexpectedly empty")
	}
	if src.Attrs.Extra["language"] != "go" {
		t.Errorf("main.go language attribute missing under profile=code: Extra=%v", src.Attrs.Extra)
	}
}

// TestWalk_UnknownProfile_NoOp confirms an unrecognised profile
// value (or empty) leaves walks unchanged — the helper returns nil
// from skipPrefixesForProfile and BuildAttributesWith sees an empty
// prefix list.
func TestWalk_UnknownProfile_NoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc x() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := search.Walk(context.Background(), search.Options{
		Root:              dir,
		Expr:              "is_source",
		Workers:           1,
		Profile:           "asparagus", // unknown, no-op
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result; got %d", len(res))
	}
	// Unknown profile means full parse — language attribute populates.
	if res[0].Attrs == nil || res[0].Attrs.Extra["language"] != "go" {
		t.Errorf("unknown profile should leave parse untouched; Extra=%v", res[0].Attrs)
	}
}

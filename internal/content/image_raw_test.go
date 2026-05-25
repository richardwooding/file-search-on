package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// TestRawImageDispatchByExtension confirms every registered RAW
// extension dispatches to the right content_type via the extension
// pass — empty files are enough because extension match beats magic.
func TestRawImageDispatchByExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".cr2", "image/raw-cr2"},
		{".cr3", "image/raw-cr3"},
		{".nef", "image/raw-nef"},
		{".nrw", "image/raw-nef"},
		{".arw", "image/raw-arw"},
		{".srf", "image/raw-arw"},
		{".sr2", "image/raw-arw"},
		{".dng", "image/raw-dng"},
		{".raf", "image/raw-raf"},
		{".orf", "image/raw-orf"},
		{".ori", "image/raw-orf"},
		{".rw2", "image/raw-rw2"},
	}
	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			path := filepath.Join(dir, "photo"+tc.ext)
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect: nil for %s", path)
			}
			if ct.Name() != tc.want {
				t.Errorf("Detect(%s) = %s, want %s", tc.ext, ct.Name(), tc.want)
			}
		})
	}
}

// TestRawImageMagicDispatch covers the two formats that register
// vendor-specific magic — RAF and ORF — so files with stripped
// extensions still dispatch correctly.
func TestRawImageMagicDispatch(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"raf-fuji", []byte("FUJIFILMCCD-RAW    "), "image/raw-raf"},
		{"orf-iiro", []byte("IIRO\x08\x00\x00\x00"), "image/raw-orf"},
		{"orf-iirs", []byte("IIRS\x08\x00\x00\x00"), "image/raw-orf"},
		{"orf-mmor", []byte("MMOR\x00\x00\x00\x08"), "image/raw-orf"},
	}
	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".bin")
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect: nil for %s", path)
			}
			if ct.Name() != tc.want {
				t.Errorf("Detect(%s) = %s, want %s", tc.name, ct.Name(), tc.want)
			}
		})
	}
}

// TestRawImageAttributesStamp confirms raw_kind and raw_vendor are
// populated from the content-type registration regardless of whether
// imagemeta successfully decoded EXIF — they're the fixed-config
// stamps, not parsed values.
func TestRawImageAttributesStamp(t *testing.T) {
	tests := []struct {
		ext        string
		wantKind   string
		wantVendor string
	}{
		{".cr2", "cr2", "canon"},
		{".cr3", "cr3", "canon"},
		{".nef", "nef", "nikon"},
		{".arw", "arw", "sony"},
		{".dng", "dng", "adobe"},
		{".raf", "raf", "fujifilm"},
		{".orf", "orf", "olympus"},
		{".rw2", "rw2", "panasonic"},
	}
	dir := t.TempDir()
	ctx := t.Context()
	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			path := filepath.Join(dir, "photo"+tc.ext)
			// Empty file is enough — Attributes returns the stamped
			// raw_kind/raw_vendor even when imagemeta can't read the body.
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect: nil for %s", path)
			}
			attrs, err := attributesAt(ctx, ct, path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["raw_kind"]; got != tc.wantKind {
				t.Errorf("raw_kind = %v, want %s", got, tc.wantKind)
			}
			if got := attrs["raw_vendor"]; got != tc.wantVendor {
				t.Errorf("raw_vendor = %v, want %s", got, tc.wantVendor)
			}
		})
	}
}

// TestRawImageNoMagicConflict guards against a future change accidentally
// registering shared TIFF magic on a RAW type and breaking image/tiff
// dispatch — a `.tif` file with II*\0 magic must still dispatch to
// image/tiff, not get hijacked by a RAW type's overlapping magic.
func TestRawImageNoMagicConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.tif")
	if err := os.WriteFile(path, []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil {
		t.Fatalf("Detect: nil")
	}
	if ct.Name() != "image/tiff" {
		t.Errorf("Detect(sample.tif) = %s, want image/tiff", ct.Name())
	}
}

// TestRawImageRegisteredInDefaultRegistry confirms every RAW format
// appears in the public type list — guards against an accidental drop
// from the init() loop.
func TestRawImageRegisteredInDefaultRegistry(t *testing.T) {
	want := map[string]bool{
		"image/raw-cr2": false,
		"image/raw-cr3": false,
		"image/raw-nef": false,
		"image/raw-arw": false,
		"image/raw-dng": false,
		"image/raw-raf": false,
		"image/raw-orf": false,
		"image/raw-rw2": false,
	}
	for _, ct := range content.DefaultRegistry().Types() {
		if _, ok := want[ct.Name()]; ok {
			want[ct.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s not registered in default registry", name)
		}
	}
}

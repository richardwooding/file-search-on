package content

import (
	"context"
	"testing"
	"testing/fstest"
)

// buildHDF5SuperblockV0 emits a minimal v0 superblock (the first 24
// bytes of the v0 layout, before the address fields). 24 bytes is
// past the Size-of-Offsets / Size-of-Lengths read positions so this
// is sufficient for the parser even though a real file would
// continue with addresses + the root group symbol table entry.
func buildHDF5SuperblockV0(offsetSize, lengthSize byte) []byte {
	b := make([]byte, 24)
	copy(b, hdf5Signature)
	b[8] = 0 // superblock version
	// Bytes 9-12: subsidiary version numbers (free-space, root-group
	// symbol-table-entry, reserved, shared-header-message). Zero is
	// the only legal value at v0 per the spec.
	b[13] = offsetSize
	b[14] = lengthSize
	// b[15] = 0 (reserved)
	// b[16..19] = group leaf/internal K (LE u16+u16) — we leave as 0,
	// the parser doesn't consult them.
	// b[20..23] = file consistency flags (LE u32) — zero.
	return b
}

// buildHDF5SuperblockV2 emits a minimal v2 superblock — 12 bytes of
// header is enough for the parser to read Size of Offsets / Size of
// Lengths.
func buildHDF5SuperblockV2(offsetSize, lengthSize byte) []byte {
	b := make([]byte, 12)
	copy(b, hdf5Signature)
	b[8] = 2
	b[9] = offsetSize
	b[10] = lengthSize
	b[11] = 0 // file consistency flags
	return b
}

// buildHDF5SuperblockV3 — same wire format as v2 but version byte = 3.
func buildHDF5SuperblockV3(offsetSize, lengthSize byte) []byte {
	b := buildHDF5SuperblockV2(offsetSize, lengthSize)
	b[8] = 3
	return b
}

func TestHDF5_DetectAndV0Attrs(t *testing.T) {
	body := buildHDF5SuperblockV0(8, 8)
	fsys := fstest.MapFS{"sample.h5": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "sample.h5")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/hdf5" {
		t.Fatalf("got %s, want science/hdf5", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "sample.h5")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"science_format":       "hdf5",
		"hdf5_format_version":  int64(0),
		"hdf5_size_of_offsets": int64(8),
		"hdf5_size_of_lengths": int64(8),
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestHDF5_V2Superblock(t *testing.T) {
	body := buildHDF5SuperblockV2(8, 8)
	fsys := fstest.MapFS{"sample.h5": {Data: body}}
	attrs, err := DefaultRegistry().Detect(fsys, "sample.h5").Attributes(context.Background(), fsys, "sample.h5")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["hdf5_format_version"]; got != int64(2) {
		t.Errorf("hdf5_format_version = %v, want 2", got)
	}
	if got := attrs["hdf5_size_of_offsets"]; got != int64(8) {
		t.Errorf("hdf5_size_of_offsets = %v, want 8", got)
	}
}

func TestHDF5_V3Superblock(t *testing.T) {
	body := buildHDF5SuperblockV3(4, 8)
	fsys := fstest.MapFS{"sample.h5": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "sample.h5").Attributes(context.Background(), fsys, "sample.h5")
	if got := attrs["hdf5_format_version"]; got != int64(3) {
		t.Errorf("hdf5_format_version = %v, want 3", got)
	}
	if got := attrs["hdf5_size_of_offsets"]; got != int64(4) {
		t.Errorf("hdf5_size_of_offsets = %v, want 4 (32-bit-era file)", got)
	}
}

func TestHDF5_UnknownVersionPreservesDetection(t *testing.T) {
	// A claimed superblock version of 99 is unknown but the magic
	// matched — we still surface hdf5_format_version so the file
	// shows up under is_hdf5.
	body := buildHDF5SuperblockV2(8, 8)
	body[8] = 99
	fsys := fstest.MapFS{"weird.h5": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "weird.h5")
	if ct == nil || ct.Name() != "science/hdf5" {
		t.Fatalf("magic-based detection failed for unknown version")
	}
	attrs, _ := ct.Attributes(context.Background(), fsys, "weird.h5")
	if got := attrs["hdf5_format_version"]; got != int64(99) {
		t.Errorf("hdf5_format_version = %v, want 99 (passed through)", got)
	}
}

func TestHDF5_BadMagicReturnsEmpty(t *testing.T) {
	body := make([]byte, 24)
	copy(body, []byte("NOT-HDF5"))
	fsys := fstest.MapFS{"x.h5": {Data: body}}
	// Detection by extension still fires; Attributes returns empty
	// when the magic doesn't match.
	ct := DefaultRegistry().Detect(fsys, "x.h5")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "x.h5")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 0 {
		t.Errorf("bad-magic file produced attrs: %v", attrs)
	}
}

func TestHDF5_TruncatedSignature(t *testing.T) {
	// Only 4 bytes of magic — must not panic.
	body := hdf5Signature[:4]
	fsys := fstest.MapFS{"trunc.h5": {Data: body}}
	if _, err := DefaultRegistry().Detect(fsys, "trunc.h5").Attributes(context.Background(), fsys, "trunc.h5"); err != nil {
		t.Errorf("truncated input errored: %v", err)
	}
}

func TestHDF5_DetectByMagicWithoutExtension(t *testing.T) {
	// File named .dat — should still detect via the 8-byte HDF5
	// magic at offset 0.
	body := buildHDF5SuperblockV2(8, 8)
	fsys := fstest.MapFS{"sim.dat": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "sim.dat")
	if ct == nil {
		t.Fatal("magic-byte detection failed")
	}
	if ct.Name() != "science/hdf5" {
		t.Errorf("got %s, want science/hdf5", ct.Name())
	}
}

func TestHDF5_OversizedOffsetSizeClamped(t *testing.T) {
	// Claim size of offsets = 200 — must NOT surface that value
	// (would break downstream parsing). The parser drops it.
	body := buildHDF5SuperblockV2(200, 8)
	fsys := fstest.MapFS{"bad.h5": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "bad.h5").Attributes(context.Background(), fsys, "bad.h5")
	if _, present := attrs["hdf5_size_of_offsets"]; present {
		t.Errorf("oversized hdf5_size_of_offsets should be dropped, got %v", attrs["hdf5_size_of_offsets"])
	}
	// Format version still surfaces.
	if got := attrs["hdf5_format_version"]; got != int64(2) {
		t.Errorf("hdf5_format_version = %v, want 2", got)
	}
}

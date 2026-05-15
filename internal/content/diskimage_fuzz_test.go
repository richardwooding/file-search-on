package content

import (
	"bytes"
	"testing"
)

// FuzzReadDMGTrailer targets the UDIF koly-trailer parser. The trailer
// is a 512-byte fixed-layout struct with offset/length fields that
// could point past EOF or wrap on overflow. We mutate the trailer
// bytes and assert: no panic, no error escape (parseDMGTrailer is
// nil-error by contract), returned virtual_size in a sane range.
func FuzzReadDMGTrailer(f *testing.F) {
	// Seed: minimal valid trailer + adversarial variants.
	good := make([]byte, dmgTrailerSize)
	copy(good, "koly")
	f.Add(good)

	short := make([]byte, 100) // smaller than trailer size
	copy(short, "koly")
	f.Add(short)

	bad := make([]byte, dmgTrailerSize)
	copy(bad, "junk")
	f.Add(bad)

	huge := make([]byte, dmgTrailerSize)
	copy(huge, "koly")
	// SectorCount near 2^63 (would overflow virtual_size).
	for i := 0x1EC; i < 0x1EC+8; i++ {
		huge[i] = 0xFF
	}
	f.Add(huge)

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseDMGTrailer(data)
		if vs, ok := attrs["virtual_size"].(int64); ok && vs < 0 {
			t.Fatalf("virtual_size went negative on adversarial input: %d", vs)
		}
	})
}

// FuzzReadQCOW2Header targets the QCOW2 BE-uint header parser. The
// header packs version + virtual_size + cluster_bits + crypt_method
// in a 72-byte struct; we mutate everything and assert no panic plus
// non-negative virtual_size.
func FuzzReadQCOW2Header(f *testing.F) {
	good := make([]byte, qcow2HeaderSize)
	copy(good, []byte{'Q', 'F', 'I', 0xFB})
	f.Add(good)

	// Truncated.
	f.Add([]byte{'Q', 'F', 'I', 0xFB, 0x00, 0x00, 0x00, 0x03})

	// Adversarial sizes — every byte 0xFF means virtual_size
	// claims 2^64-1, which int64-cast goes negative; the parser
	// must clamp.
	huge := make([]byte, qcow2HeaderSize)
	copy(huge, []byte{'Q', 'F', 'I', 0xFB})
	for i := 0x18; i < 0x18+8; i++ {
		huge[i] = 0xFF
	}
	f.Add(huge)

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseQCOW2Header(data)
		if vs, ok := attrs["virtual_size"].(int64); ok && vs < 0 {
			t.Fatalf("virtual_size went negative: %d", vs)
		}
		if cb, ok := attrs["cluster_bits"].(int64); ok && cb < 0 {
			t.Fatalf("cluster_bits went negative: %d", cb)
		}
	})
}

// FuzzReadVHDXMetadata targets the VHDX region-table → metadata
// region walker. The walker reads a region table (signature, entry
// count, per-entry GUIDs + offsets + lengths) and a metadata table
// (signature, entry count, item-id GUIDs + offsets + lengths). Both
// have entry-count caps and bounds-checked offsets — exactly the
// territory where bounds-check bugs hide. We drive the two functions
// separately so the fuzzer can isolate which input shape triggers a
// failure.
func FuzzReadVHDXMetadata(f *testing.F) {
	// Seed for findVHDXMetadataRegion: a well-formed table with one
	// Metadata-region entry.
	rt := make([]byte, vhdxRegionTableSize)
	copy(rt, "regi")
	rt[8] = 1 // entryCount = 1
	copy(rt[16:32], vhdxMetadataRegionGUID)
	// FileOffset at rt[32:40] = 0; Length at rt[40:44] = 1024.
	rt[40] = 0x00
	rt[41] = 0x04
	f.Add(rt)

	// Adversarial: claimed entryCount > 2047 (must reject).
	rtPath := make([]byte, vhdxRegionTableSize)
	copy(rtPath, "regi")
	rtPath[8] = 0xFF
	rtPath[9] = 0xFF
	rtPath[10] = 0xFF
	rtPath[11] = 0xFF
	f.Add(rtPath)

	// Truncated region table.
	f.Add([]byte("regi\x00\x00\x00\x00\x01"))

	// Bytes that look like a metadata region (used to seed the
	// second walker via the bytes.Reader hack below).
	mt := make([]byte, 256)
	copy(mt, "metadata")
	mt[0x0A] = 1 // entryCount = 1
	copy(mt[0x20:0x30], vhdxVirtualDiskSizeGUID)
	// valueOffset = 0x100, length = 8 — points past the buffer,
	// must reject.
	mt[0x30] = 0x00
	mt[0x31] = 0x01
	mt[0x34] = 8
	f.Add(mt)

	f.Fuzz(func(t *testing.T, data []byte) {
		// findVHDXMetadataRegion — input is a region-table sized
		// buffer; if shorter, the function should reject. The length
		// it returns is the value the file claims; the walker is
		// responsible for bounds-checking before reading. A large
		// uint32 length is a legitimate value, not corruption — we
		// only assert against negative offsets here (which would
		// indicate the parser dropped its int64(uint64) guard).
		off, _ := findVHDXMetadataRegion(data)
		if off < 0 {
			t.Fatalf("findVHDXMetadataRegion returned negative offset: %d", off)
		}

		// findVHDXVirtualDiskSize — feed the same bytes; the walker
		// should reject buffers that don't lead with "metadata".
		size := findVHDXVirtualDiskSize(data)
		if size < 0 {
			t.Fatalf("findVHDXVirtualDiskSize returned negative: %d", size)
		}

		// readVHDXVirtualSize wraps both — drive it via a
		// bytes.Reader so the seek/read paths get exercised too.
		size = readVHDXVirtualSize(bytes.NewReader(data), int64(len(data)))
		if size < 0 {
			t.Fatalf("readVHDXVirtualSize returned negative: %d", size)
		}
	})
}

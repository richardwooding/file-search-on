package content

import (
	"testing"
)

// FuzzParseHDF5Superblock targets the binary header walker. The
// superblock packs format version + Size of Offsets / Lengths in
// fixed positions that differ between v0/v1 and v2/v3 — exactly the
// territory where bounds-check bugs hide. Fuzz body asserts no panic
// and no obviously-bogus surface (negative format version, claimed
// offset size exceeding the cap).
func FuzzParseHDF5Superblock(f *testing.F) {
	// Seed 1: valid v0 superblock.
	f.Add(buildHDF5SuperblockV0(8, 8))

	// Seed 2: valid v2 superblock with 8-byte offsets.
	f.Add(buildHDF5SuperblockV2(8, 8))

	// Seed 3: v3 with 32-bit-era 4-byte offsets.
	f.Add(buildHDF5SuperblockV3(4, 8))

	// Seed 4: claimed version = 99 (unknown).
	weird := buildHDF5SuperblockV2(8, 8)
	weird[8] = 99
	f.Add(weird)

	// Seed 5: signature valid but oversized Size of Offsets (200) —
	// parser must clamp without panicking.
	big := buildHDF5SuperblockV2(200, 200)
	f.Add(big)

	// Seed 6: truncated to first 4 bytes of signature.
	f.Add(hdf5Signature[:4])

	// Seed 7: all-0xFF noise the size of a v0 superblock.
	bad := make([]byte, 64)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	// Seed 8: empty input.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseHDF5Superblock(data)
		if v, ok := attrs["hdf5_format_version"].(int64); ok && v < 0 {
			t.Fatalf("hdf5_format_version went negative: %d", v)
		}
		if v, ok := attrs["hdf5_size_of_offsets"].(int64); ok {
			if v <= 0 || v > hdf5MaxOffsetSize {
				t.Fatalf("hdf5_size_of_offsets out of bounds: %d (cap %d)", v, hdf5MaxOffsetSize)
			}
		}
		if v, ok := attrs["hdf5_size_of_lengths"].(int64); ok {
			if v <= 0 || v > hdf5MaxLengthSize {
				t.Fatalf("hdf5_size_of_lengths out of bounds: %d (cap %d)", v, hdf5MaxLengthSize)
			}
		}
	})
}

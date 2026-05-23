package content

import (
	"encoding/binary"
	"testing"
)

// FuzzParseWALHeader targets the 32-byte SQLite WAL header parser
// plus the frame-count estimator. The header field offsets are tight
// (every parse is a fixed-offset BigEndian.Uint32) so the high-value
// adversarial inputs are: short buffers, header claiming page_size=0
// or 1 (would blow frame-count division if unguarded), magic bytes
// almost-matching but not quite, and absurdly large file sizes
// paired with tiny page sizes.
func FuzzParseWALHeader(f *testing.F) {
	// Seed 1: valid BE-checksum header, page_size 4096.
	be := make([]byte, sqliteWALHeaderLen)
	copy(be, sqliteWALMagicBE)
	binary.BigEndian.PutUint32(be[4:8], 3007000)
	binary.BigEndian.PutUint32(be[8:12], 4096)
	binary.BigEndian.PutUint32(be[12:16], 1)
	f.Add(be, int64(32+(24+4096)*5)) // 5 frames

	// Seed 2: valid LE-checksum header, page_size 65536.
	le := make([]byte, sqliteWALHeaderLen)
	copy(le, sqliteWALMagicLE)
	binary.BigEndian.PutUint32(le[4:8], 3007000)
	binary.BigEndian.PutUint32(le[8:12], 65536)
	f.Add(le, int64(32+24+65536)) // 1 frame

	// Seed 3: truncated buffer (smaller than 32 bytes) — must not
	// panic on the BigEndian.Uint32 calls.
	f.Add([]byte{0x37, 0x7F, 0x06, 0x82, 0x00, 0x2D, 0xE2, 0x18}, int64(0))

	// Seed 4: all 0xFF junk — magic mismatch, must return empty attrs.
	junk := make([]byte, sqliteWALHeaderLen)
	for i := range junk {
		junk[i] = 0xFF
	}
	f.Add(junk, int64(1<<40))

	// Seed 5: valid magic, page_size=1 (sub-512, would crater
	// frame-count math without the sanity clamp).
	bad := make([]byte, sqliteWALHeaderLen)
	copy(bad, sqliteWALMagicBE)
	binary.BigEndian.PutUint32(bad[8:12], 1)
	f.Add(bad, int64(1<<40))

	// Seed 6: valid magic, page_size=3 (non-power-of-two — must be
	// rejected by walFrameCount).
	npt := make([]byte, sqliteWALHeaderLen)
	copy(npt, sqliteWALMagicBE)
	binary.BigEndian.PutUint32(npt[8:12], 3)
	f.Add(npt, int64(10_000))

	// Seed 7: valid magic, claims page_size=0x80000000 (overflow if
	// frame_size arithmetic is done in int32).
	huge := make([]byte, sqliteWALHeaderLen)
	copy(huge, sqliteWALMagicBE)
	binary.BigEndian.PutUint32(huge[8:12], 0x80000000)
	f.Add(huge, int64(1<<40))

	// Seed 8: nil-ish buffer (zero-length) — bounds check at entry
	// must catch it.
	f.Add([]byte{}, int64(0))

	f.Fuzz(func(t *testing.T, data []byte, fileSize int64) {
		// Hard contract: never panic, regardless of input.
		attrs := parseSQLiteWALHeader(data, fileSize)
		// When attrs are populated, byte_order is exactly "be" or "le".
		if bo, ok := attrs["sqlite_wal_byte_order"].(string); ok {
			if bo != "be" && bo != "le" {
				t.Fatalf("sqlite_wal_byte_order = %q, want be/le", bo)
			}
		}
		// frame_count must never be negative — the file_size guard
		// in walFrameCount short-circuits before division.
		if fc, ok := attrs["sqlite_wal_frame_count"].(int64); ok && fc < 0 {
			t.Fatalf("sqlite_wal_frame_count negative: %d", fc)
		}
		// page_size must never surface as negative either.
		if ps, ok := attrs["sqlite_wal_page_size"].(int64); ok && ps < 0 {
			t.Fatalf("sqlite_wal_page_size negative: %d", ps)
		}
	})
}

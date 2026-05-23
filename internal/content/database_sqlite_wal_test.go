package content

import (
	"context"
	"encoding/binary"
	"testing"
	"testing/fstest"
)

// buildWALHeader synthesises a 32-byte WAL header. magic selects the
// checksum byte-order variant; all other multi-byte fields are stored
// big-endian per the WAL spec regardless of magic.
func buildWALHeader(magic []byte, formatVersion, pageSize, checkpointSeq uint32) []byte {
	b := make([]byte, sqliteWALHeaderLen)
	copy(b, magic)
	binary.BigEndian.PutUint32(b[4:8], formatVersion)
	binary.BigEndian.PutUint32(b[8:12], pageSize)
	binary.BigEndian.PutUint32(b[12:16], checkpointSeq)
	return b
}

func TestParseWALHeader_BigEndianMagic(t *testing.T) {
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 4096, 7)
	// File holds the 32-byte header + 3 frames (each 24 + 4096 bytes).
	fileSize := int64(sqliteWALHeaderLen + 3*(sqliteWALFrameHeader+4096))

	attrs := parseSQLiteWALHeader(header, fileSize)

	if got := attrs["database_format"]; got != "sqlite-wal" {
		t.Errorf("database_format = %v, want sqlite-wal", got)
	}
	if got := attrs["sqlite_wal_byte_order"]; got != "be" {
		t.Errorf("sqlite_wal_byte_order = %v, want be", got)
	}
	if got := attrs["sqlite_wal_format_version"]; got != int64(3007000) {
		t.Errorf("sqlite_wal_format_version = %v, want 3007000", got)
	}
	if got := attrs["sqlite_wal_page_size"]; got != int64(4096) {
		t.Errorf("sqlite_wal_page_size = %v, want 4096", got)
	}
	if got := attrs["sqlite_wal_checkpoint_seq"]; got != int64(7) {
		t.Errorf("sqlite_wal_checkpoint_seq = %v, want 7", got)
	}
	if got := attrs["sqlite_wal_frame_count"]; got != int64(3) {
		t.Errorf("sqlite_wal_frame_count = %v, want 3", got)
	}
}

func TestParseWALHeader_LittleEndianMagic(t *testing.T) {
	header := buildWALHeader(sqliteWALMagicLE, 3007000, 8192, 42)
	attrs := parseSQLiteWALHeader(header, int64(sqliteWALHeaderLen+sqliteWALFrameHeader+8192))

	if got := attrs["sqlite_wal_byte_order"]; got != "le" {
		t.Errorf("sqlite_wal_byte_order = %v, want le", got)
	}
	if got := attrs["sqlite_wal_page_size"]; got != int64(8192) {
		t.Errorf("sqlite_wal_page_size = %v, want 8192", got)
	}
	if got := attrs["sqlite_wal_frame_count"]; got != int64(1) {
		t.Errorf("sqlite_wal_frame_count = %v, want 1", got)
	}
}

func TestParseWALHeader_MagicMismatch(t *testing.T) {
	junk := make([]byte, sqliteWALHeaderLen)
	for i := range junk {
		junk[i] = 0xCC
	}
	attrs := parseSQLiteWALHeader(junk, 1<<20)
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs on magic mismatch, got %v", attrs)
	}
}

func TestParseWALHeader_TruncatedHeader(t *testing.T) {
	short := []byte{0x37, 0x7F, 0x06, 0x82, 0x00, 0x2D, 0xE2, 0x18}
	attrs := parseSQLiteWALHeader(short, int64(len(short)))
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs on truncated header, got %v", attrs)
	}
}

func TestParseWALHeader_HeaderOnlyFile(t *testing.T) {
	// A WAL file containing just the 32-byte header (no frames) is
	// what SQLite writes immediately after a checkpoint that empties
	// the WAL. frame_count should be 0, not absent.
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 4096, 12)
	attrs := parseSQLiteWALHeader(header, int64(sqliteWALHeaderLen))

	if got := attrs["sqlite_wal_frame_count"]; got != int64(0) {
		t.Errorf("sqlite_wal_frame_count = %v, want 0", got)
	}
}

func TestParseWALHeader_InvalidPageSize(t *testing.T) {
	// Page size of 1 is below the spec minimum (512). frame_count
	// should be absent rather than a fantastically large number.
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 1, 0)
	attrs := parseSQLiteWALHeader(header, int64(1<<30))

	if _, ok := attrs["sqlite_wal_frame_count"]; ok {
		t.Errorf("expected sqlite_wal_frame_count absent for page_size=1")
	}
	// Other surfaced fields should still be present — the header
	// parses, we just can't count frames.
	if got := attrs["sqlite_wal_page_size"]; got != int64(1) {
		t.Errorf("sqlite_wal_page_size = %v, want 1 (raw value surfaced)", got)
	}
}

func TestParseWALHeader_NonPowerOfTwoPageSize(t *testing.T) {
	// Spec says page sizes are powers of two — non-power-of-two
	// values are adversarial. Reject for frame-count purposes.
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 4097, 0)
	attrs := parseSQLiteWALHeader(header, int64(1<<20))

	if _, ok := attrs["sqlite_wal_frame_count"]; ok {
		t.Errorf("expected sqlite_wal_frame_count absent for non-power-of-2 page_size")
	}
}

func TestSQLiteWALType_Detection(t *testing.T) {
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 4096, 1)
	fs := fstest.MapFS{
		"places.db-wal": &fstest.MapFile{Data: header},
	}

	wt := &sqliteWALType{}
	attrs, err := wt.Attributes(context.Background(), fs, "places.db-wal")
	if err != nil {
		t.Fatalf("Attributes returned error: %v", err)
	}
	if got := attrs["sqlite_wal_byte_order"]; got != "be" {
		t.Errorf("sqlite_wal_byte_order = %v, want be", got)
	}
}

func TestSQLiteWALType_RegistryDetectionByExtension(t *testing.T) {
	header := buildWALHeader(sqliteWALMagicBE, 3007000, 4096, 1)
	fs := fstest.MapFS{
		"foo.db-wal":      &fstest.MapFile{Data: header},
		"bar.sqlite-wal":  &fstest.MapFile{Data: header},
		"baz.sqlite3-wal": &fstest.MapFile{Data: header},
	}
	reg := DefaultRegistry()

	for _, name := range []string{"foo.db-wal", "bar.sqlite-wal", "baz.sqlite3-wal"} {
		ct := reg.Detect(fs, name)
		if ct == nil {
			t.Errorf("Detect(%q) = nil, want database/sqlite-wal", name)
			continue
		}
		if ct.Name() != "database/sqlite-wal" {
			t.Errorf("Detect(%q) = %q, want database/sqlite-wal", name, ct.Name())
		}
	}
}

func TestSQLiteWALType_RegistryDetectionByMagic(t *testing.T) {
	// Magic-only detection (file with non-canonical extension) —
	// both BE and LE magic variants should fire.
	beHeader := buildWALHeader(sqliteWALMagicBE, 3007000, 4096, 1)
	leHeader := buildWALHeader(sqliteWALMagicLE, 3007000, 4096, 1)
	fs := fstest.MapFS{
		"random-be.bin": &fstest.MapFile{Data: beHeader},
		"random-le.bin": &fstest.MapFile{Data: leHeader},
	}
	reg := DefaultRegistry()

	for _, name := range []string{"random-be.bin", "random-le.bin"} {
		ct := reg.Detect(fs, name)
		if ct == nil {
			t.Errorf("Detect(%q) = nil, want database/sqlite-wal", name)
			continue
		}
		if ct.Name() != "database/sqlite-wal" {
			t.Errorf("Detect(%q) = %q, want database/sqlite-wal", name, ct.Name())
		}
	}
}

func TestWALFrameCount(t *testing.T) {
	tests := []struct {
		name     string
		fileSize int64
		pageSize int64
		want     int64
		wantOK   bool
	}{
		{"header-only", 32, 4096, 0, true},
		{"one-frame", 32 + 24 + 4096, 4096, 1, true},
		{"five-frames", 32 + 5*(24+4096), 4096, 5, true},
		{"partial-trailing-frame", 32 + 5*(24+4096) + 100, 4096, 5, true},
		{"undersized-file", 16, 4096, 0, false},
		{"page-size-zero", 1 << 20, 0, 0, false},
		{"page-size-too-small", 1 << 20, 256, 0, false},
		{"page-size-too-large", 1 << 30, 131072, 0, false},
		{"page-size-npt", 1 << 20, 4097, 0, false},
		{"page-size-max", 32 + 24 + 65536, 65536, 1, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := walFrameCount(tc.fileSize, tc.pageSize)
			if ok != tc.wantOK {
				t.Fatalf("walFrameCount(%d, %d) ok = %v, want %v",
					tc.fileSize, tc.pageSize, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("walFrameCount(%d, %d) = %d, want %d",
					tc.fileSize, tc.pageSize, got, tc.want)
			}
		})
	}
}

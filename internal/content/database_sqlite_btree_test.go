package content

import (
	"encoding/binary"
	"testing"
)

// TestDecodeVarint covers the SQLite varint format described in
// https://www.sqlite.org/fileformat.html#varint — 1 to 9 bytes, big
// endian, high-bit continuation on bytes 0-7 with byte 8 (if
// present) using all 8 bits.
func TestDecodeVarint(t *testing.T) {
	cases := []struct {
		name    string
		input   []byte
		want    uint64
		wantN   int
		wantErr bool
	}{
		{"1-byte zero", []byte{0x00}, 0, 1, false},
		{"1-byte small", []byte{0x7F}, 127, 1, false},
		{"2-byte boundary", []byte{0x81, 0x00}, 128, 2, false},
		{"2-byte mid", []byte{0x82, 0x05}, 0x105, 2, false},
		{"3-byte", []byte{0x81, 0x80, 0x00}, 1 << 14, 3, false},
		{"4-byte", []byte{0x81, 0x80, 0x80, 0x00}, 1 << 21, 4, false},
		{"9-byte full", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 0xFFFFFFFFFFFFFFFF, 9, false},
		{"empty", []byte{}, 0, 0, true},
		{"truncated continuation", []byte{0x80}, 0, 0, true},
		// 8 continuation bytes claim a 9-byte varint but the 9th
		// byte is missing.
		{"truncated at byte 9", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n := decodeVarint(tc.input)
			if tc.wantErr {
				if n != 0 {
					t.Errorf("expected zero n on error, got %d (val=%d)", n, got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("value = %d, want %d", got, tc.want)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
		})
	}
}

// TestDecodeRecord exercises the SQLite record decoder (§2.1) with a
// hand-built three-column record covering the most common serial
// types we see in sqlite_master: text, int, text.
func TestDecodeRecord(t *testing.T) {
	// Build a record with columns: ("table", 42, "CREATE TABLE x").
	col0 := "table"            // text(5) → serial = 5*2+13 = 23
	col1 := int8(42)           // i8 → serial = 1
	col2 := "CREATE TABLE x"   // text(14) → serial = 14*2+13 = 41

	header := []byte{}
	// header_size will be 4 (header_size itself + 3 type bytes), then
	// we patch it in after we know.
	header = append(header, 0x00) // placeholder for header_size
	header = append(header, byte((len(col0)*2)+13))
	header = append(header, 0x01) // int8 serial
	header = append(header, byte((len(col2)*2)+13))
	header[0] = byte(len(header))

	body := append([]byte{}, col0...)
	body = append(body, byte(col1))
	body = append(body, col2...)

	payload := append(header, body...)

	vals, err := decodeRecord(payload, []int{0, 1, 2})
	if err != nil {
		t.Fatalf("decodeRecord err: %v", err)
	}
	if len(vals) != 3 {
		t.Fatalf("want 3 cols, got %d", len(vals))
	}
	if got := textOf(vals[0]); got != col0 {
		t.Errorf("col0 = %q, want %q", got, col0)
	}
	if vals[1].Kind != recordInt || vals[1].Int != int64(col1) {
		t.Errorf("col1 = %+v, want int=%d", vals[1], col1)
	}
	if got := textOf(vals[2]); got != col2 {
		t.Errorf("col2 = %q, want %q", got, col2)
	}
}

// TestDecodeRecord_SparseProjection confirms we can ask for just the
// columns we care about (sqlite_master row: skip cols 2 and 3).
func TestDecodeRecord_SparseProjection(t *testing.T) {
	// Build a record with 5 cols mimicking sqlite_master:
	// (type, name, tbl_name, rootpage, sql). Project cols 0, 1, 4.
	cols := [][]byte{
		[]byte("table"), // 0
		[]byte("users"), // 1
		[]byte("users"), // 2 (tbl_name)
		// col 3 (rootpage) = int1 value 2
		nil,
		[]byte("CREATE TABLE users (id INTEGER)"), // 4
	}
	header := []byte{0x00}
	for i, c := range cols {
		if i == 3 {
			header = append(header, 0x01) // int8
			continue
		}
		header = append(header, byte(len(c)*2+13))
	}
	header[0] = byte(len(header))

	body := []byte{}
	for i, c := range cols {
		if i == 3 {
			body = append(body, 0x02) // rootpage = 2
			continue
		}
		body = append(body, c...)
	}
	payload := append(header, body...)

	vals, err := decodeRecord(payload, []int{0, 1, 4})
	if err != nil {
		t.Fatalf("decodeRecord err: %v", err)
	}
	if got := textOf(vals[0]); got != "table" {
		t.Errorf("col0 = %q, want table", got)
	}
	if got := textOf(vals[1]); got != "users" {
		t.Errorf("col1 = %q, want users", got)
	}
	if got := textOf(vals[2]); got != "CREATE TABLE users (id INTEGER)" {
		t.Errorf("col2 = %q", got)
	}
}

// TestDecodeRecord_AdversarialHeader confirms a header that claims
// more bytes than the payload provides returns an error rather than
// panicking.
func TestDecodeRecord_AdversarialHeader(t *testing.T) {
	// Header claims size 200 but payload is only 10 bytes.
	payload := []byte{200, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := decodeRecord(payload, []int{0})
	if err == nil {
		t.Fatal("expected error on adversarial header size")
	}
}

// TestColumnSize spot-checks the size table for the column-skip
// path. Covers each branch of columnSize.
func TestColumnSize(t *testing.T) {
	cases := []struct {
		serial uint64
		want   int
	}{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 6}, {6, 8}, {7, 8},
		{8, 0}, {9, 0},
		{12, 0},        // blob, 0 bytes
		{14, 1},        // blob, 1 byte
		{13, 0},        // text, 0 bytes
		{15, 1},        // text, 1 byte
		{12 + 2*100, 100}, // blob, 100 bytes
		{13 + 2*255, 255}, // text, 255 bytes
	}
	for _, tc := range cases {
		if got := columnSize(tc.serial); got != tc.want {
			t.Errorf("columnSize(%d) = %d, want %d", tc.serial, got, tc.want)
		}
	}
}

// TestWalkSQLiteMaster_MalformedPageReturnsCleanly confirms the
// walker doesn't panic on a page whose b-tree header has bogus
// values (zero cells but garbage page type, etc.).
func TestWalkSQLiteMaster_MalformedPageReturnsCleanly(t *testing.T) {
	page := make([]byte, 4096)
	// Write valid SQLite header so we reach the b-tree.
	copy(page, sqliteMagic)
	binary.BigEndian.PutUint16(page[16:18], 4096) // page size
	page[18] = 1                                    // format version
	// Byte 100 = page type — set to bogus value.
	page[100] = 0xFF

	called := false
	err := walkSQLiteMaster(page, 4096, func(row sqliteMasterRow) {
		called = true
	})
	if err != nil {
		t.Errorf("expected nil err on unknown page type, got %v", err)
	}
	if called {
		t.Error("visit should not be called on unknown page type")
	}
}

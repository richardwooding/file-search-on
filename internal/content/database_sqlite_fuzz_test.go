package content

import (
	"testing"
)

// FuzzParseSQLiteHeader targets the 100-byte SQLite v3 header walker.
// Fixed-offset binary parser — exactly the territory where bounds-
// check bugs hide. Fuzz body asserts no panic and no negative numeric
// surfaces (page_size, page_count, schema_version, user_version,
// application_id all parse as unsigned).
func FuzzParseSQLiteHeader(f *testing.F) {
	// Seed 1: valid full header.
	f.Add(buildSQLiteHeader(4096, 1, 10, 7, sqliteEncodingUTF8, 42, 0x0FACADE0))

	// Seed 2: WAL mode + large page size sentinel.
	f.Add(buildSQLiteHeader(sqlitePageSizeMagic, 2, 1000000, 999, sqliteEncodingUTF16LE, 0xFFFFFFFF, 0xFFFFFFFF))

	// Seed 3: magic only (truncated past byte 16).
	f.Add(append([]byte{}, sqliteMagic...))

	// Seed 4: empty input.
	f.Add([]byte{})

	// Seed 5: all 0xFF noise — wrong magic.
	bad := make([]byte, 256)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	// Seed 6: byte 17 corrupted to 0 (page_size = 0, which is invalid
	// per the spec but must not crash the parser).
	zero := buildSQLiteHeader(0, 1, 0, 0, sqliteEncodingUTF8, 0, 0)
	f.Add(zero)

	// Seed 7: spec-illegal page_size = 257 (not a power of 2 ≥ 512;
	// real SQLite would refuse to open this, but our parser must
	// surface what's in the header without crashing).
	weird := buildSQLiteHeader(257, 1, 0, 0, sqliteEncodingUTF8, 0, 0)
	f.Add(weird)

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseSQLiteHeader(data)
		for _, key := range []string{"sqlite_page_size", "sqlite_page_count", "sqlite_schema_version", "sqlite_user_version"} {
			if v, ok := attrs[key].(int64); ok && v < 0 {
				t.Fatalf("%s went negative: %d", key, v)
			}
		}
	})
}

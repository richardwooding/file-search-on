package content

import (
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"maps"
)

// SQLite v3 file format constants per the SQLite Database File Format
// reference (https://www.sqlite.org/fileformat.html). The 100-byte
// header is dense and well-specified; we parse it directly without
// pulling in modernc.org/sqlite or any other runtime dependency.
//
// Schema introspection (walking sqlite_master to enumerate table /
// view / index / trigger names + a schema fingerprint) is explicitly
// out of scope for v1 — it requires a b-tree walker with varint
// decoding, freeblock-aware page scanning, and the right ~500 LOC of
// careful work. The 100-byte header alone gives agents reliable
// is_sqlite detection plus version + size + encoding filtering,
// which is the headline use case.

var sqliteMagic = []byte("SQLite format 3\x00")

const (
	sqliteMagicLen  = 16
	sqliteHeaderLen = 100

	// sqliteReadCap bounds disk reads. 1 MiB covers the SQLite header
	// (100 bytes) plus enough b-tree pages for sqlite_master to walk
	// schema introspection — typical schemas fit in one or two pages
	// at the default 4 KiB page size, and even 16 KiB page schemas
	// rarely exceed 64 pages. Files where sqlite_master spans past
	// 1 MiB surface header-only attrs; the schema walk degrades
	// gracefully (returns nothing rather than erroring).
	sqliteReadCap = 1 << 20

	// sqlitePageSizeMagic is the encoding for the 65536-byte page
	// size — the page-size field is uint16, can't represent 65536
	// directly, so the spec reserves the value 0x0001 to mean 65536.
	sqlitePageSizeMagic = 1

	sqliteEncodingUTF8    = 1
	sqliteEncodingUTF16LE = 2
	sqliteEncodingUTF16BE = 3
)

func init() {
	Register(&sqliteType{})
}

// sqliteType registers the database/sqlite content type. SQLite is
// the most-deployed database in the world — every iOS / Android app,
// every browser, every CLI with a local store. Files named .db /
// .sqlite / .sqlite3 / .db3 are common under ~/Library, ~/.config,
// and app-data directories.
type sqliteType struct{}

func (s *sqliteType) Name() string         { return "database/sqlite" }
func (s *sqliteType) Extensions() []string { return []string{".db", ".sqlite", ".sqlite3", ".db3"} }
func (s *sqliteType) MagicBytes() [][]byte { return [][]byte{sqliteMagic} }

// Attributes reads the 100-byte SQLite header and surfaces version /
// size / encoding fields. SQLCipher / encrypted databases don't
// expose the SQLite magic bytes (they're encrypted along with the
// rest of the file), so encrypted DBs detect as binary noise — we
// can't help without the key.
func (s *sqliteType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readSQLiteInfo(fsys, path)
}

func readSQLiteInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, sqliteReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseSQLiteHeader(buf), nil
}

// LookupSQLiteAppName is the exported registry hook called from the
// celexpr layer after the SQLite content type's Attributes parse.
// Lives there (not inside ContentType.Attributes) because path-based
// registry dimensions need the file's absolute display path, while
// ContentType.Attributes receives only the fs.FS-relative fsPath
// (which loses everything above the search root).
//
// Returns the matched app name or "" when no registry entry fires.
// Adding a new entry is a one-line struct literal in
// `internal/content/database_sqlite_registry.go`.
func LookupSQLiteAppName(extras Attributes, displayPath string) string {
	return lookupAppName(extras, displayPath)
}

// parseSQLiteHeader walks the 100-byte SQLite v3 header. Pure
// function — fuzz target exercises it directly. Returns empty attrs
// on magic mismatch; returns just the database_format discriminator
// on truncation past the magic but before the full header.
//
// Layout per https://www.sqlite.org/fileformat.html §1.3:
//
//	0-15   Magic "SQLite format 3\0"
//	16-17  Page size (BE u16) — value 1 means 65536
//	18     File format write version (1 = legacy, 2 = WAL)
//	19     File format read version
//	20     Reserved space per page
//	21-23  Payload-fraction constants
//	24-27  File change counter (BE u32)
//	28-31  Page count (BE u32)
//	32-35  First freelist trunk page
//	36-39  Total freelist pages
//	40-43  Schema cookie (BE u32) — `PRAGMA schema_version`
//	44-47  Schema format number
//	48-51  Default page cache size
//	52-55  Largest root b-tree page (vacuum mode)
//	56-59  Text encoding (BE u32) — 1=UTF-8, 2=UTF-16le, 3=UTF-16be
//	60-63  User version (BE u32) — `PRAGMA user_version`
//	64-67  Incremental vacuum mode
//	68-71  Application ID (BE u32) — `PRAGMA application_id`
//	72-91  Reserved (zero)
//	92-95  Version-valid-for number
//	96-99  SQLITE_VERSION_NUMBER of the last writer
func parseSQLiteHeader(data []byte) Attributes {
	if len(data) < sqliteMagicLen {
		return Attributes{}
	}
	for i, b := range sqliteMagic {
		if data[i] != b {
			return Attributes{}
		}
	}
	if len(data) < sqliteHeaderLen {
		return databaseAttrs("sqlite", nil)
	}

	pageSize := sqlitePageSize(data)
	extras := Attributes{
		"sqlite_page_size":      pageSize,
		"sqlite_format_version": int64(data[18]),
		"sqlite_page_count":     int64(binary.BigEndian.Uint32(data[28:32])),
		"sqlite_schema_version": int64(binary.BigEndian.Uint32(data[40:44])),
		"sqlite_text_encoding":  sqliteEncodingName(binary.BigEndian.Uint32(data[56:60])),
		"sqlite_user_version":   int64(binary.BigEndian.Uint32(data[60:64])),
		"sqlite_application_id": int64(binary.BigEndian.Uint32(data[68:72])),
	}
	// Schema introspection: walk sqlite_master if we have enough
	// bytes for at least page 1. Best-effort — failures don't drop
	// the header attrs.
	if int64(len(data)) >= pageSize {
		maps.Copy(extras, schemaFromSQLiteMaster(data, pageSize))
	}
	return databaseAttrs("sqlite", extras)
}

// sqlitePageSize decodes the page-size field, honouring the special
// 0x0001 sentinel for 65536-byte pages.
func sqlitePageSize(data []byte) int64 {
	raw := binary.BigEndian.Uint16(data[16:18])
	if raw == sqlitePageSizeMagic {
		return 65536
	}
	return int64(raw)
}

// sqliteEncodingName maps the text-encoding integer to the canonical
// short name. Unknown values pass through as "" so the attribute is
// just absent rather than surfaced as a bare integer.
func sqliteEncodingName(enc uint32) string {
	switch enc {
	case sqliteEncodingUTF8:
		return "utf-8"
	case sqliteEncodingUTF16LE:
		return "utf-16le"
	case sqliteEncodingUTF16BE:
		return "utf-16be"
	}
	return ""
}

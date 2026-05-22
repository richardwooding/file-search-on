package content

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// execLookPath / execCommand / osDirFS are tiny shims so the test
// imports stay tidy when adding the real-DB tests below.
var (
	execLookPath = exec.LookPath
	execCommand  = exec.Command
	osDirFS      = os.DirFS
)

// buildSQLiteHeader synthesises a 100-byte SQLite v3 header with the
// fields we surface set to the caller's values. Reserved bytes are
// left as zero — the parser doesn't care.
func buildSQLiteHeader(pageSizeRaw uint16, writeVersion byte, pageCount, schemaCookie, encoding, userVersion, applicationID uint32) []byte {
	b := make([]byte, sqliteHeaderLen)
	copy(b, sqliteMagic)
	binary.BigEndian.PutUint16(b[16:18], pageSizeRaw)
	b[18] = writeVersion
	b[19] = writeVersion
	b[21] = 64
	b[22] = 32
	b[23] = 32
	binary.BigEndian.PutUint32(b[28:32], pageCount)
	binary.BigEndian.PutUint32(b[40:44], schemaCookie)
	binary.BigEndian.PutUint32(b[56:60], encoding)
	binary.BigEndian.PutUint32(b[60:64], userVersion)
	binary.BigEndian.PutUint32(b[68:72], applicationID)
	return b
}

func TestSQLite_FullDetectAndAttrs(t *testing.T) {
	body := buildSQLiteHeader(4096, 1, 10, 7, sqliteEncodingUTF8, 42, 0x0FACADE0)
	fsys := fstest.MapFS{"app.db": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "app.db")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "database/sqlite" {
		t.Fatalf("got %s, want database/sqlite", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "app.db")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"database_format":       "sqlite",
		"sqlite_page_size":      int64(4096),
		"sqlite_format_version": int64(1),
		"sqlite_page_count":     int64(10),
		"sqlite_schema_version": int64(7),
		"sqlite_text_encoding":  "utf-8",
		"sqlite_user_version":   int64(42),
		"sqlite_application_id": int64(0x0FACADE0),
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestSQLite_WALWriteVersion(t *testing.T) {
	body := buildSQLiteHeader(8192, 2, 0, 0, sqliteEncodingUTF8, 0, 0)
	fsys := fstest.MapFS{"x.sqlite": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.sqlite").Attributes(context.Background(), fsys, "x.sqlite")
	if got := attrs["sqlite_format_version"]; got != int64(2) {
		t.Errorf("sqlite_format_version = %v, want 2 (WAL)", got)
	}
	if got := attrs["sqlite_page_size"]; got != int64(8192) {
		t.Errorf("sqlite_page_size = %v, want 8192", got)
	}
}

func TestSQLite_LargePageSizeMagic(t *testing.T) {
	// 0x0001 is the spec's special sentinel for a 65536-byte page.
	body := buildSQLiteHeader(sqlitePageSizeMagic, 1, 0, 0, sqliteEncodingUTF8, 0, 0)
	fsys := fstest.MapFS{"big.db": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "big.db").Attributes(context.Background(), fsys, "big.db")
	if got := attrs["sqlite_page_size"]; got != int64(65536) {
		t.Errorf("sqlite_page_size = %v, want 65536 (sentinel-decoded)", got)
	}
}

func TestSQLite_UTF16Encodings(t *testing.T) {
	for _, tc := range []struct {
		enc  uint32
		want string
	}{
		{sqliteEncodingUTF16LE, "utf-16le"},
		{sqliteEncodingUTF16BE, "utf-16be"},
	} {
		body := buildSQLiteHeader(4096, 1, 0, 0, tc.enc, 0, 0)
		fsys := fstest.MapFS{"x.db": {Data: body}}
		attrs, _ := DefaultRegistry().Detect(fsys, "x.db").Attributes(context.Background(), fsys, "x.db")
		if got := attrs["sqlite_text_encoding"]; got != tc.want {
			t.Errorf("encoding %d → %q, want %q", tc.enc, got, tc.want)
		}
	}
}

func TestSQLite_UnknownEncodingOmitted(t *testing.T) {
	body := buildSQLiteHeader(4096, 1, 0, 0, 99, 0, 0)
	fsys := fstest.MapFS{"x.db": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.db").Attributes(context.Background(), fsys, "x.db")
	if got := attrs["sqlite_text_encoding"]; got != "" {
		t.Errorf("unknown encoding should produce empty string, got %v", got)
	}
}

func TestSQLite_DetectByMagicWithoutExtension(t *testing.T) {
	// .ext extension isn't registered — magic still fires.
	body := buildSQLiteHeader(4096, 1, 0, 0, sqliteEncodingUTF8, 0, 0)
	fsys := fstest.MapFS{"unnamed.ext": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "unnamed.ext")
	if ct == nil {
		t.Fatal("magic-byte detection failed")
	}
	if ct.Name() != "database/sqlite" {
		t.Errorf("got %s, want database/sqlite", ct.Name())
	}
}

func TestSQLite_BadMagicReturnsEmpty(t *testing.T) {
	body := make([]byte, sqliteHeaderLen)
	copy(body, []byte("NOT-SQLITE-FILE!"))
	fsys := fstest.MapFS{"x.db": {Data: body}}
	attrs, err := DefaultRegistry().Detect(fsys, "x.db").Attributes(context.Background(), fsys, "x.db")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 0 {
		t.Errorf("bad-magic file produced attrs: %v", attrs)
	}
}

func TestSQLite_TruncatedAfterMagicSurfacesFormatOnly(t *testing.T) {
	// Only the 16-byte magic — too short for the 100-byte header.
	// Detection succeeds; database_format surfaces as the sentinel.
	body := append([]byte{}, sqliteMagic...)
	fsys := fstest.MapFS{"t.db": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "t.db").Attributes(context.Background(), fsys, "t.db")
	if got := attrs["database_format"]; got != "sqlite" {
		t.Errorf("database_format = %v, want 'sqlite'", got)
	}
	if _, present := attrs["sqlite_page_size"]; present {
		t.Errorf("sqlite_page_size should be absent on truncated header")
	}
}

func TestSQLite_TruncatedBelowMagic(t *testing.T) {
	body := sqliteMagic[:8] // 8 bytes — less than the 16-byte magic
	fsys := fstest.MapFS{"x.db": {Data: body}}
	if _, err := DefaultRegistry().Detect(fsys, "x.db").Attributes(context.Background(), fsys, "x.db"); err != nil {
		t.Errorf("truncated input errored: %v", err)
	}
}

// TestSQLite_SchemaIntrospection_RealDB exercises the sqlite_master
// b-tree walker against a real database created via the `sqlite3`
// shell. Skips when sqlite3 isn't on PATH — CI runners on both
// macOS and Ubuntu have it preinstalled. Catches integration bugs
// the hand-built unit tests miss (cell-pointer math, real CREATE
// statement formatting, autoindex generation).
func TestSQLite_SchemaIntrospection_RealDB(t *testing.T) {
	if _, err := execLookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 shell not in PATH")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cmd := execCommand("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(`
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE history (id INTEGER, url TEXT, ts INTEGER);
CREATE INDEX idx_history_ts ON history(ts);
CREATE VIEW v_recent AS SELECT * FROM history ORDER BY ts DESC LIMIT 100;
CREATE TRIGGER trg_users AFTER INSERT ON users BEGIN INSERT INTO history(id) VALUES (NEW.id); END;
`)
	if err := cmd.Run(); err != nil {
		t.Fatalf("sqlite3 run: %v", err)
	}

	fsys := osDirFS(dir)
	ct := DefaultRegistry().Detect(fsys, "app.db")
	if ct == nil || ct.Name() != "database/sqlite" {
		t.Fatalf("Detect = %v, want database/sqlite", ct)
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "app.db")
	if err != nil {
		t.Fatal(err)
	}

	// Schema attrs.
	if got := attrs["sqlite_table_count"]; got != int64(2) {
		t.Errorf("sqlite_table_count = %v, want 2 (users, history)", got)
	}
	if got := attrs["sqlite_view_count"]; got != int64(1) {
		t.Errorf("sqlite_view_count = %v, want 1 (v_recent)", got)
	}
	if got := attrs["sqlite_index_count"]; got == nil {
		t.Errorf("sqlite_index_count missing (expected ≥1 — idx_history_ts)")
	}
	if got := attrs["sqlite_trigger_count"]; got != int64(1) {
		t.Errorf("sqlite_trigger_count = %v, want 1 (trg_users)", got)
	}

	names, ok := attrs["sqlite_table_names"].([]string)
	if !ok {
		t.Fatalf("sqlite_table_names not []string: %T", attrs["sqlite_table_names"])
	}
	if len(names) != 2 || names[0] != "history" || names[1] != "users" {
		t.Errorf("sqlite_table_names = %v, want [history users]", names)
	}

	fp, ok := attrs["sqlite_schema_fingerprint"].(string)
	if !ok || len(fp) != 64 {
		t.Errorf("sqlite_schema_fingerprint = %q (len %d), want 64-char hex", fp, len(fp))
	}
}

// TestSQLite_SchemaFingerprintStableAcrossCosmetics verifies that
// the schema fingerprint is invariant under cosmetic reorders of
// CREATE statements (we sort by (type, name) before hashing).
func TestSQLite_SchemaFingerprintStableAcrossCosmetics(t *testing.T) {
	if _, err := execLookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 shell not in PATH")
	}
	buildDB := func(sql string) string {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "x.db")
		cmd := execCommand("sqlite3", dbPath)
		cmd.Stdin = strings.NewReader(sql)
		if err := cmd.Run(); err != nil {
			t.Fatalf("sqlite3 run: %v", err)
		}
		fsys := osDirFS(dir)
		attrs, err := DefaultRegistry().Detect(fsys, "x.db").Attributes(context.Background(), fsys, "x.db")
		if err != nil {
			t.Fatal(err)
		}
		return attrs["sqlite_schema_fingerprint"].(string)
	}
	a := buildDB("CREATE TABLE a (x INT); CREATE TABLE b (y INT);")
	b := buildDB("CREATE TABLE b (y INT); CREATE TABLE a (x INT);")
	if a != b {
		t.Errorf("fingerprint should be stable across creation order; got %q vs %q", a, b)
	}
}

func TestSQLite_ApplicationIDExample(t *testing.T) {
	// Firefox's places.sqlite stamps application_id = 0x0FACADE0.
	// Confirms an agent can filter by app stamp.
	body := buildSQLiteHeader(32768, 2, 100, 1, sqliteEncodingUTF8, 0, 0x0FACADE0)
	fsys := fstest.MapFS{"places.sqlite": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "places.sqlite").Attributes(context.Background(), fsys, "places.sqlite")
	if got := attrs["sqlite_application_id"]; got != int64(0x0FACADE0) {
		t.Errorf("sqlite_application_id = %v, want 0x0FACADE0", got)
	}
}

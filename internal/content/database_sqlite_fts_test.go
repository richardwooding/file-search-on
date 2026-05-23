package content

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

// TestParseSQLiteHeader_FTSDetection creates a real SQLite database
// with an FTS5 virtual table, reads its bytes back into memory, and
// confirms the b-tree-based schema walker surfaces sqlite_fts_*
// attributes. End-to-end exercise of:
//
//  1. The CREATE VIRTUAL TABLE statement landing in sqlite_master.
//  2. The walker's text scan classifying that row as FTS via
//     isFTSCreate (delegates to ftsCreatePattern).
//  3. The aggregator surfacing sqlite_fts_table_count +
//     sqlite_fts_table_names.
//
// Uses modernc.org/sqlite (already pulled in for body extraction) to
// generate the fixture — no separate dep for the test path.
func TestParseSQLiteHeader_FTSDetection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fts.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE history USING fts5(url, title)`); err != nil {
		t.Fatalf("CREATE VIRTUAL TABLE: %v", err)
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE notes USING fts5(body)`); err != nil {
		t.Fatalf("CREATE VIRTUAL TABLE: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE plain (id INTEGER, body TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	_ = db.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	attrs := parseSQLiteHeader(data)

	if got, _ := attrs["sqlite_fts_table_count"].(int64); got != 2 {
		t.Errorf("sqlite_fts_table_count = %v, want 2", got)
	}
	names, _ := attrs["sqlite_fts_table_names"].([]string)
	wantNames := map[string]bool{"history": false, "notes": false}
	for _, n := range names {
		if _, ok := wantNames[n]; ok {
			wantNames[n] = true
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("sqlite_fts_table_names missing %q (got %v)", n, names)
		}
	}
	// The non-FTS table must NOT appear in sqlite_fts_table_names.
	for _, n := range names {
		if n == "plain" {
			t.Errorf("sqlite_fts_table_names contains non-FTS table %q", n)
		}
	}
}

// TestParseSQLiteHeader_NoFTSTablesOmitsAttrs confirms the FTS
// attributes are absent (not zero-valued) when the DB has no FTS
// tables — agents inspecting JSON output rely on `omitempty` to
// distinguish "no FTS" from "empty FTS table list".
func TestParseSQLiteHeader_NoFTSTablesOmitsAttrs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	_ = db.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	attrs := parseSQLiteHeader(data)
	if _, ok := attrs["sqlite_fts_table_count"]; ok {
		t.Errorf("sqlite_fts_table_count should be absent for non-FTS DBs, got %v",
			attrs["sqlite_fts_table_count"])
	}
	if _, ok := attrs["sqlite_fts_table_names"]; ok {
		t.Errorf("sqlite_fts_table_names should be absent for non-FTS DBs")
	}
}

// TestSqliteType_FTSDetectionViaRegistry confirms the end-to-end
// pipeline — registry detection + Attributes returning the FTS attrs
// — fires when an FTS-using DB is encountered during a walk.
func TestSqliteType_FTSDetectionViaRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE chat USING fts5(body)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	_ = db.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	fsys := fstest.MapFS{"fixture.db": {Data: data}}

	ct := DefaultRegistry().Detect(fsys, "fixture.db")
	if ct == nil {
		t.Fatal("registry.Detect returned nil")
	}
	if ct.Name() != "database/sqlite" {
		t.Fatalf("ct.Name() = %s, want database/sqlite", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "fixture.db")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got, _ := attrs["sqlite_fts_table_count"].(int64); got != 1 {
		t.Errorf("sqlite_fts_table_count = %v, want 1", got)
	}
}

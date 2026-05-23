package content

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// buildFTSFixture creates a temp SQLite file with an FTS5 virtual
// table populated with the given rows. Returns the file path. Uses
// the same modernc.org/sqlite driver the production extractor uses
// so the test is end-to-end realistic.
func buildFTSFixture(t *testing.T, table string, columns []string, rows [][]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	colDDL := strings.Join(columns, ", ")
	createStmt := `CREATE VIRTUAL TABLE "` + table + `" USING fts5(` + colDDL + `)`
	if _, err := db.Exec(createStmt); err != nil {
		t.Fatalf("CREATE VIRTUAL TABLE: %v", err)
	}

	placeholders := strings.Repeat("?, ", len(columns))
	placeholders = strings.TrimSuffix(placeholders, ", ")
	insertStmt := `INSERT INTO "` + table + `" VALUES (` + placeholders + `)`
	for _, row := range rows {
		args := make([]any, len(row))
		for i, v := range row {
			args[i] = v
		}
		if _, err := db.Exec(insertStmt, args...); err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}
	return path
}

func TestSqliteBody_FTS5(t *testing.T) {
	path := buildFTSFixture(t, "messages",
		[]string{"sender", "body"},
		[][]string{
			{"alice", "hello kubernetes pods"},
			{"bob", "anyone running transformers on gpu?"},
			{"alice", "just deployed the istio mesh"},
		})

	body, err := sqliteBody(context.Background(), path, 1<<20)
	if err != nil {
		t.Fatalf("sqliteBody: %v", err)
	}
	for _, want := range []string{
		"alice", "bob", "kubernetes", "transformers", "istio mesh",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestSqliteBody_NoFTSReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	// Plain non-FTS table — body extractor should return empty.
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO notes VALUES (1, 'hello world')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	_ = db.Close()

	body, err := sqliteBody(context.Background(), path, 1<<20)
	if err != nil {
		t.Fatalf("sqliteBody: %v", err)
	}
	if body != "" {
		t.Errorf("expected empty body for non-FTS DB, got %q", body)
	}
}

func TestSqliteBody_RespectsMaxBytes(t *testing.T) {
	path := buildFTSFixture(t, "logs",
		[]string{"line"},
		[][]string{
			{"transformer architectures dominate llm inference"},
			{"kubernetes operators reconcile cluster state"},
			{"sqlite is the most-deployed database in the world"},
		})

	body, err := sqliteBody(context.Background(), path, 30)
	if err != nil {
		t.Fatalf("sqliteBody: %v", err)
	}
	if len(body) > 30 {
		t.Errorf("body length %d exceeds cap 30", len(body))
	}
}

func TestSqliteBody_EmptyPathReturnsEmpty(t *testing.T) {
	body, err := sqliteBody(context.Background(), "", 1<<20)
	if err != nil {
		t.Fatalf("sqliteBody: %v", err)
	}
	if body != "" {
		t.Errorf("expected empty body for empty path, got %q", body)
	}
}

func TestSqliteBody_NonexistentFileReturnsEmpty(t *testing.T) {
	body, err := sqliteBody(context.Background(), "/nonexistent/path/xyz.db", 1<<20)
	if err != nil {
		t.Fatalf("sqliteBody: %v", err)
	}
	if body != "" {
		t.Errorf("expected empty body for missing file, got %q", body)
	}
}

func TestIsFTSCreate(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{`CREATE VIRTUAL TABLE messages USING fts5(body)`, true},
		{`CREATE VIRTUAL TABLE x USING fts4(content)`, true},
		{`create  virtual  table  IF NOT EXISTS  notes  using  fts5  (body, tags)`, true},
		{`CREATE TABLE plain (id INTEGER, body TEXT)`, false},
		{`CREATE INDEX idx ON foo(bar)`, false},
		// Adversarial: substring in a string literal — but our walker
		// only ever sees real CREATE statements from sqlite_master so
		// this case is academic. The pattern requires VIRTUAL TABLE.
		{`SELECT 'using fts5' FROM x`, false},
		// fts5vocab is a known SQLite module name — must NOT match.
		{`CREATE VIRTUAL TABLE v USING fts5vocab(messages, 'col')`, false},
	}
	for _, tc := range tests {
		got := isFTSCreate(tc.sql)
		if got != tc.want {
			t.Errorf("isFTSCreate(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

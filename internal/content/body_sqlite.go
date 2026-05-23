package content

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// sqliteBody extracts concatenated text from every FTS3 / FTS4 / FTS5
// virtual table's `_content` shadow in a SQLite database, capped at
// maxBytes. Returns "" when the DB has no FTS tables, the file isn't
// readable as SQLite, or extraction hits an error — same "best effort"
// contract as the other structured body extractors.
//
// Requires a real OS path: opens the DB via modernc.org/sqlite (pure-Go
// SQLite driver) using `mode=ro&immutable=1` so we never acquire write
// locks and never touch the journal / WAL files. Archive-walk paths
// (in-memory fs.FS) cannot reach this path — callers gate on path
// availability before invoking.
//
// Layout per https://www.sqlite.org/fts5.html#external_content_tables:
//
//   - Standard FTS5: `<fts>_content` shadow holds the indexed text in
//     columns c0, c1, c2, ... plus a primary-key id column.
//   - External-content FTS5 (`content=<source>`): `<fts>_content` is
//     empty (just a vestigial schema); real text lives in <source>.
//     The `content=` option is recorded in `<fts>_config` rows where
//     `k='content'`. We follow that pointer.
//   - FTS3 / FTS4: simpler — `<fts>_content` always holds the text
//     directly. We treat them the same as standard FTS5.
//
// Error handling: any SQL error short-circuits to "" (silent — the file
// may be SQLCipher-encrypted, locked by another process, or have an
// unknown shadow-table convention). Cancellation propagates via ctx.
func sqliteBody(ctx context.Context, osPath string, maxBytes int) (string, error) {
	if osPath == "" {
		return "", nil
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20 // 1 MiB default; caller normally overrides
	}

	// mode=ro + immutable=1: no journal, no WAL, no locks. The
	// immutable flag tells SQLite the file won't change underneath us
	// (true for the duration of one Open) so it skips lock acquisition
	// entirely — safe for a read-only walker visiting many files.
	dsn := "file:" + url.PathEscape(osPath) + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", nil //nolint:nilerr
	}
	defer func() { _ = db.Close() }()

	// One-shot ping with ctx so a non-SQLite file (mis-detected, e.g.
	// encrypted) errors fast instead of hanging on a bad open.
	if err := db.PingContext(ctx); err != nil {
		return "", nil //nolint:nilerr
	}

	ftsTables, err := listFTSTables(ctx, db)
	if err != nil || len(ftsTables) == 0 {
		return "", err //nolint:nilerr // empty body, no FTS tables
	}

	var sb strings.Builder
	for _, ft := range ftsTables {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if sb.Len() >= maxBytes {
			break
		}
		source := ft.contentSource(ctx, db)
		if err := appendFTSContent(ctx, db, &sb, source, maxBytes); err != nil {
			// One table erroring out doesn't sink the whole extraction;
			// just continue to the next.
			continue
		}
	}
	out := sb.String()
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out, nil
}

// ftsTable holds the bare info we need to walk one FTS virtual table's
// content. The `name` is the user-visible virtual-table name; the
// content shadow lives at `<name>_content` by default.
type ftsTable struct {
	name string
}

// contentSource resolves the table that actually holds the indexed
// text. For standard FTS this is `<name>_content`; for external-
// content FTS5 (declared with `content=<source>`) it's the user-named
// source table. Falls back to `<name>_content` on any read error.
func (f ftsTable) contentSource(ctx context.Context, db *sql.DB) string {
	defaultSrc := f.name + "_content"
	cfgTable := f.name + "_config"
	// `<fts>_config` only exists for FTS5; FTS3/4 just use the
	// default shadow.
	row := db.QueryRowContext(ctx,
		`SELECT v FROM "`+sqliteIdent(cfgTable)+`" WHERE k = 'content' LIMIT 1`)
	var v string
	if err := row.Scan(&v); err != nil {
		return defaultSrc
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultSrc
	}
	return v
}

// appendFTSContent SELECTs every text-shaped column from the given
// content table and appends each row's joined text to sb (newline-
// separated). Stops when sb hits maxBytes. Schema introspection via
// PRAGMA table_info — robust across FTS versions and external-content
// renames.
func appendFTSContent(ctx context.Context, db *sql.DB, sb *strings.Builder, source string, maxBytes int) error {
	cols, err := textColumns(ctx, db, source)
	if err != nil || len(cols) == 0 {
		return err
	}
	// Build a SELECT projecting only the text columns. Quoted with
	// double-quotes per the SQL standard so column names containing
	// reserved words or whitespace don't break the statement.
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = `"` + sqliteIdent(c) + `"`
	}
	query := `SELECT ` + strings.Join(parts, ", ") +
		` FROM "` + sqliteIdent(source) + `"`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	scanTargets := make([]any, len(cols))
	scanValues := make([]sql.NullString, len(cols))
	for i := range scanTargets {
		scanTargets[i] = &scanValues[i]
	}

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rows.Scan(scanTargets...); err != nil {
			continue
		}
		for _, v := range scanValues {
			if !v.Valid || v.String == "" {
				continue
			}
			if sb.Len() >= maxBytes {
				return nil
			}
			sb.WriteString(v.String)
			sb.WriteByte('\n')
		}
	}
	return rows.Err()
}

// listFTSTables returns every FTS3/4/5 virtual table in the schema.
// Single SQL pass — no need to re-read sqlite_master via the b-tree
// walker because we already have a SQL connection.
func listFTSTables(ctx context.Context, db *sql.DB) ([]ftsTable, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, sql FROM sqlite_master WHERE type='table' AND sql IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ftsTable
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			continue
		}
		if ftsCreatePattern.MatchString(ddl) {
			out = append(out, ftsTable{name: name})
		}
	}
	return out, rows.Err()
}

// ftsCreatePattern matches `CREATE VIRTUAL TABLE ... USING fts<N>`.
// Case-insensitive; word-boundary after the version digit so future
// modules whose names start with `fts5` don't false-positive.
var ftsCreatePattern = regexp.MustCompile(`(?i)create\s+virtual\s+table[^;]*using\s+fts[345]\b`)

// textColumns returns the names of every TEXT-typed column on the
// given table per PRAGMA table_info. Non-text columns (INTEGER /
// BLOB / REAL) are excluded because the FTS payload is text-shaped;
// rowid / docid integer columns would just dilute the body.
//
// PRAGMA table_info columns: cid, name, type, notnull, dflt_value, pk.
func textColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	q := fmt.Sprintf(`PRAGMA table_info("%s")`, sqliteIdent(table))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var (
			cid     int64
			name    string
			ctype   string
			notnull int64
			dflt    sql.NullString
			pk      int64
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			continue
		}
		// SQLite type affinity: anything containing "CHAR", "CLOB",
		// or "TEXT" is TEXT. Empty type (FTS virtual columns) also
		// counts as text — the FTS engine returns text rows for those.
		upper := strings.ToUpper(ctype)
		if ctype == "" ||
			strings.Contains(upper, "CHAR") ||
			strings.Contains(upper, "CLOB") ||
			strings.Contains(upper, "TEXT") {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

// sqliteIdent escapes a SQLite identifier so it can be safely
// interpolated into a `"..."` quoted name. SQLite uses the doubled-
// quote convention: `foo"bar` becomes `foo""bar`. Conservative — we
// never accept arbitrary user input here (callers pass schema-
// extracted names), but defence in depth is cheap.
func sqliteIdent(name string) string {
	return strings.ReplaceAll(name, `"`, `""`)
}

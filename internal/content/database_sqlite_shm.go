package content

import (
	"context"
	"io/fs"
)

// SQLite SHM (shared memory) sidecar — the mmap'd index used by
// multiple connections to coordinate WAL access. Lives next to its
// parent database as `<basename>-shm`. The on-disk format is loose
// and implementation-defined (SQLite documents it as transient
// shared memory), so detection is extension-only and we surface no
// extra attributes beyond the cross-format `database_format` tag.
//
// The mere presence of an SHM sidecar tells an agent that the parent
// SQLite database is in WAL mode AND has at least one connection
// active (or recently active). For forensic triage that's enough —
// poking inside the SHM would require reverse-engineering the WAL
// index structure for no actionable gain.

func init() {
	Register(&sqliteSHMType{})
}

// sqliteSHMType registers the database/sqlite-shm content type.
type sqliteSHMType struct{}

func (s *sqliteSHMType) Name() string { return "database/sqlite-shm" }
func (s *sqliteSHMType) Extensions() []string {
	return []string{".db-shm", ".sqlite-shm", ".sqlite3-shm"}
}
func (s *sqliteSHMType) MagicBytes() [][]byte { return nil }

// Attributes is a no-op beyond surfacing the database_format tag —
// the SHM file's on-disk layout isn't a stable contract.
func (s *sqliteSHMType) Attributes(_ context.Context, _ fs.FS, _ string) (Attributes, error) {
	return databaseAttrs("sqlite-shm", nil), nil
}

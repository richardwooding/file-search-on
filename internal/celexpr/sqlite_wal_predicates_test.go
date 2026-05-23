package celexpr_test

import (
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestSQLiteTrioPredicates verifies that is_sqlite_wal / is_sqlite_shm
// fire for the sidecar content types AND that is_sqlite / is_database
// deliberately do NOT — per the issue #176 contract, the sidecars
// accompany a database, they aren't one.
func TestSQLiteTrioPredicates(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		extra       content.Attributes
		// Expected predicate state for the four database flags.
		isSQLite    bool
		isSQLiteWAL bool
		isSQLiteSHM bool
		isDatabase  bool
	}{
		{
			name:        "sqlite-db",
			contentType: "database/sqlite",
			isSQLite:    true,
			isDatabase:  true,
		},
		{
			name:        "sqlite-wal-sidecar",
			contentType: "database/sqlite-wal",
			extra: content.Attributes{
				"database_format":           "sqlite-wal",
				"sqlite_wal_format_version": int64(3007000),
				"sqlite_wal_page_size":      int64(4096),
				"sqlite_wal_byte_order":     "be",
			},
			isSQLiteWAL: true,
		},
		{
			name:        "sqlite-shm-sidecar",
			contentType: "database/sqlite-shm",
			extra: content.Attributes{
				"database_format": "sqlite-shm",
			},
			isSQLiteSHM: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := celexpr.AssembleAttributes(
				"placeholder", "/tmp/"+tc.name, "/tmp",
				".db", tc.contentType, 1024, time.Now(), tc.extra,
			)
			if attrs.IsSQLite != tc.isSQLite {
				t.Errorf("IsSQLite = %v, want %v", attrs.IsSQLite, tc.isSQLite)
			}
			if attrs.IsSQLiteWAL != tc.isSQLiteWAL {
				t.Errorf("IsSQLiteWAL = %v, want %v", attrs.IsSQLiteWAL, tc.isSQLiteWAL)
			}
			if attrs.IsSQLiteSHM != tc.isSQLiteSHM {
				t.Errorf("IsSQLiteSHM = %v, want %v", attrs.IsSQLiteSHM, tc.isSQLiteSHM)
			}
			if attrs.IsDatabase != tc.isDatabase {
				t.Errorf("IsDatabase = %v, want %v", attrs.IsDatabase, tc.isDatabase)
			}
		})
	}
}

// TestSQLiteWALAttributesViaCEL exercises the full CEL pipeline:
// build attrs from an Extra map, then evaluate an expression that
// references each of the new variables.
func TestSQLiteWALAttributesViaCEL(t *testing.T) {
	eval, err := celexpr.New(`is_sqlite_wal && sqlite_wal_page_size == 4096 && sqlite_wal_byte_order == "be" && sqlite_wal_frame_count > 0`)
	if err != nil {
		t.Fatal(err)
	}

	attrs := celexpr.AssembleAttributes(
		"places.db-wal", "/tmp/places.db-wal", "/tmp",
		".db-wal", "database/sqlite-wal", 32+24+4096, time.Now(),
		content.Attributes{
			"database_format":           "sqlite-wal",
			"sqlite_wal_format_version": int64(3007000),
			"sqlite_wal_page_size":      int64(4096),
			"sqlite_wal_checkpoint_seq": int64(1),
			"sqlite_wal_frame_count":    int64(1),
			"sqlite_wal_byte_order":     "be",
		},
	)

	match, err := eval.Evaluate(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected match against full WAL predicate, got none")
	}
}

// TestSQLiteSidecarsExcludedFromIsDatabase confirms the negative
// half of the contract: an `is_database` CEL filter must NOT match
// WAL / SHM sidecars.
func TestSQLiteSidecarsExcludedFromIsDatabase(t *testing.T) {
	eval, err := celexpr.New("is_database")
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range []string{"database/sqlite-wal", "database/sqlite-shm"} {
		attrs := celexpr.AssembleAttributes(
			"x", "/tmp/x", "/tmp", ".db-wal", ct, 32, time.Now(), nil,
		)
		match, err := eval.Evaluate(attrs)
		if err != nil {
			t.Fatal(err)
		}
		if match {
			t.Errorf("is_database matched %s, expected no match", ct)
		}
	}
}

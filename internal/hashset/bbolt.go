package hashset

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

const (
	boltBucketMD5    = "md5"
	boltBucketSHA1   = "sha1"
	boltBucketSHA256 = "sha256"
	boltBucketMeta   = "meta"
	boltSchemaKey    = "schema_version"
	boltSchemaValue  = "1"
)

// boltSet is the bbolt-backed Set. Three buckets (md5/sha1/sha256)
// store raw 16/20/32-byte hash bytes as keys; values are empty
// (bucket membership is the signal). Random-access lookup is
// O(log N) — fast enough that an NSRL allowlist with 50M entries
// doesn't dominate forensic walks where each file is already paying
// the hash compute cost.
type boltSet struct {
	db     *bbolt.DB
	counts map[string]int
	once   sync.Once
}

// OpenBolt opens an existing bbolt-format hashset file for read-only
// queries. Build the file via the `hash-set build` CLI subcommand
// (see internal/hashset/builder.go).
//
// The file is opened with bbolt's read-only mode so a search walk
// can't accidentally mutate the allowlist. ErrSchemaMismatch
// surfaces when the file's schema_version meta key doesn't match
// the binary — protects against future format changes.
func OpenBolt(path string) (Set, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	db, err := bbolt.Open(abs, 0o600, &bbolt.Options{
		Timeout:  5 * time.Second,
		ReadOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open hashset: %w", err)
	}
	if err := validateBoltSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	bs := &boltSet{db: db}
	if err := bs.computeCounts(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("count entries: %w", err)
	}
	return bs, nil
}

// ErrSchemaMismatch is returned by OpenBolt when the file's
// `meta/schema_version` key isn't the current "1".
var ErrSchemaMismatch = errors.New("hashset: schema version mismatch (rebuild via `hash-set build`)")

func validateBoltSchema(db *bbolt.DB) error {
	return db.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(boltBucketMeta))
		if meta == nil {
			return ErrSchemaMismatch
		}
		v := meta.Get([]byte(boltSchemaKey))
		if string(v) != boltSchemaValue {
			return ErrSchemaMismatch
		}
		// All three algo buckets must exist (may be empty).
		for _, b := range []string{boltBucketMD5, boltBucketSHA1, boltBucketSHA256} {
			if tx.Bucket([]byte(b)) == nil {
				return ErrSchemaMismatch
			}
		}
		return nil
	})
}

func (b *boltSet) computeCounts() error {
	counts := make(map[string]int, 3)
	return b.db.View(func(tx *bbolt.Tx) error {
		for _, algo := range Algorithms {
			bk := tx.Bucket([]byte(algo))
			if bk != nil {
				counts[algo] = bk.Stats().KeyN
			}
		}
		b.counts = counts
		return nil
	})
}

func (b *boltSet) Contains(algo, hexHash string) bool {
	raw, err := hex.DecodeString(hexHash)
	if err != nil {
		return false
	}
	var found bool
	_ = b.db.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(algo))
		if bk == nil {
			return nil
		}
		if v := bk.Get(raw); v != nil {
			found = true
		}
		return nil
	})
	return found
}

func (b *boltSet) Counts() map[string]int {
	// Return a defensive copy — callers shouldn't mutate the
	// cached count map.
	out := make(map[string]int, len(b.counts))
	maps.Copy(out, b.counts)
	return out
}

func (b *boltSet) Close() error {
	var closeErr error
	b.once.Do(func() {
		closeErr = b.db.Close()
	})
	return closeErr
}

// Open auto-detects the file format: a bbolt file (with the right
// schema metadata) loads via OpenBolt; everything else falls
// through to LoadTextFile. Useful for CLI / MCP code paths that
// accept either format transparently.
//
// Detection strategy: try OpenBolt first; on ErrSchemaMismatch OR
// any bbolt open error, fall back to text. This catches both real
// text files AND bbolt files written by a different binary version
// (the latter retries through text, fails clearly, and prompts the
// user to rebuild).
func Open(path string) (Set, error) {
	if set, err := OpenBolt(path); err == nil {
		return set, nil
	}
	return LoadTextFile(path)
}

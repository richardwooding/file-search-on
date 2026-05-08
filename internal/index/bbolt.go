package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

const (
	schemaVersion = 1
	bucketAttrs   = "attrs_v1"
	bucketMeta    = "meta"
	metaKey       = "schema"

	// putBufferSize is the depth of the write channel between workers
	// and the single writer goroutine. Bigger = more burst absorption,
	// smaller = quicker back-pressure into Stats.Errors.
	putBufferSize = 256
	// flushInterval bounds writer goroutine latency: even with one
	// straggler in the channel we batch+commit at least this often.
	flushInterval = 100 * time.Millisecond
	// flushBatch is the inner batch size — at most this many puts go
	// into a single bbolt batch transaction.
	flushBatch = 64
)

type metaPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Encoding      string `json:"encoding"`
}

type boltIndex struct {
	db    *bbolt.DB
	puts  chan putReq
	wg    sync.WaitGroup
	once  sync.Once
	stats memoryStats
}

type putReq struct {
	key string
	val []byte
}

func openBoltIndex(path string) (*boltIndex, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve index path: %w", err)
	}
	db, err := bbolt.Open(abs, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open index file: %w", err)
	}

	if err := initOrValidateSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	idx := &boltIndex{
		db:   db,
		puts: make(chan putReq, putBufferSize),
	}
	idx.wg.Add(1)
	go idx.writerLoop()
	return idx, nil
}

func initOrValidateSchema(db *bbolt.DB) error {
	return db.Update(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(bucketMeta))
		if meta == nil {
			// Fresh file: create both buckets and stamp schema metadata.
			meta, err := tx.CreateBucket([]byte(bucketMeta))
			if err != nil {
				return fmt.Errorf("create meta bucket: %w", err)
			}
			payload, err := json.Marshal(metaPayload{
				SchemaVersion: schemaVersion,
				Encoding:      "gob",
			})
			if err != nil {
				return err
			}
			if err := meta.Put([]byte(metaKey), payload); err != nil {
				return err
			}
			if _, err := tx.CreateBucket([]byte(bucketAttrs)); err != nil {
				return fmt.Errorf("create attrs bucket: %w", err)
			}
			return nil
		}
		raw := meta.Get([]byte(metaKey))
		if raw == nil {
			return ErrSchemaMismatch
		}
		var got metaPayload
		if err := json.Unmarshal(raw, &got); err != nil {
			return ErrSchemaMismatch
		}
		if got.SchemaVersion != schemaVersion || got.Encoding != "gob" {
			return ErrSchemaMismatch
		}
		if tx.Bucket([]byte(bucketAttrs)) == nil {
			// Meta exists but data bucket doesn't — corrupt, refuse to use.
			return ErrSchemaMismatch
		}
		return nil
	})
}

func (b *boltIndex) Lookup(path string, size int64, mtime time.Time) (*Entry, bool) {
	if path == "" || mtime.IsZero() || !filepath.IsAbs(path) {
		b.stats.misses.Add(1)
		return nil, false
	}
	var e *Entry
	err := b.db.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(bucketAttrs))
		if bk == nil {
			return nil
		}
		raw := bk.Get([]byte(path))
		if raw == nil {
			return nil
		}
		decoded, decErr := decodeEntry(raw)
		if decErr != nil {
			return decErr
		}
		e = decoded
		return nil
	})
	if err != nil {
		b.stats.errors.Add(1)
		return nil, false
	}
	if e == nil {
		b.stats.misses.Add(1)
		return nil, false
	}
	if e.Size != size || e.ModTimeUnixNano != mtime.UnixNano() {
		b.stats.stales.Add(1)
		return nil, false
	}
	b.stats.hits.Add(1)
	return e, true
}

func (b *boltIndex) Put(path string, e *Entry) error {
	if path == "" || e == nil || !filepath.IsAbs(path) {
		b.stats.errors.Add(1)
		return nil
	}
	val, err := encodeEntry(e)
	if err != nil {
		b.stats.errors.Add(1)
		return nil
	}
	// Non-blocking enqueue — never throttle the walker on the cache.
	select {
	case b.puts <- putReq{key: path, val: val}:
	default:
		b.stats.errors.Add(1)
	}
	return nil
}

func (b *boltIndex) Stats() Stats {
	return Stats{
		Hits:   b.stats.hits.Load(),
		Misses: b.stats.misses.Load(),
		Puts:   b.stats.puts.Load(),
		Stales: b.stats.stales.Load(),
		Errors: b.stats.errors.Load(),
	}
}

// Close drains the writer channel, waits for the writer goroutine to exit,
// then closes the bbolt db. Safe to call once; subsequent calls are no-ops.
func (b *boltIndex) Close() error {
	var closeErr error
	b.once.Do(func() {
		close(b.puts)
		b.wg.Wait()
		closeErr = b.db.Close()
	})
	return closeErr
}

// writerLoop is the single writer. It coalesces incoming Put requests into
// bbolt batches: either flushBatch entries or flushInterval whichever
// comes first, then commits in one Update tx. Reads continue concurrently
// against db.View.
func (b *boltIndex) writerLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	pending := make([]putReq, 0, flushBatch)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		err := b.db.Update(func(tx *bbolt.Tx) error {
			bk := tx.Bucket([]byte(bucketAttrs))
			if bk == nil {
				return errors.New("attrs bucket missing")
			}
			for _, p := range pending {
				if err := bk.Put([]byte(p.key), p.val); err != nil {
					return err
				}
			}
			return nil
		})
		written := uint64(len(pending))
		if err != nil {
			b.stats.errors.Add(written)
		} else {
			b.stats.puts.Add(written)
		}
		pending = pending[:0]
	}

	for {
		select {
		case req, ok := <-b.puts:
			if !ok {
				flush()
				return
			}
			pending = append(pending, req)
			if len(pending) >= flushBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}


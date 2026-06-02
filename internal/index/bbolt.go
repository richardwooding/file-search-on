package index

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

const (
	schemaVersion     = 1
	bucketAttrs       = "attrs_v1"
	bucketBodies      = "bodies_v1"
	bucketBodyAccess  = "body_access_v1"
	bucketMeta        = "meta"
	metaKey           = "schema"
	metaBodiesSizeKey = "bodies_total_size"

	// putBufferSize is the depth of the write channel between workers
	// and the single writer goroutine. Bigger = more burst absorption,
	// smaller = quicker back-pressure into Stats.Errors.
	putBufferSize = 256
	// bodyPutBufferSize is the body-puts channel depth. Bodies can be
	// up to bodyMaxBytes (8 MiB) each, but in practice typical
	// markdown / source / DOCX bodies are 10-100 KB. 128 slots
	// comfortably absorb a parallel-walker burst (8 workers × ~16
	// bodies in flight) without dropping puts into BodyErrors; the
	// worst-case memory (128 × 8 MiB = 1 GiB) is bounded but unlikely.
	bodyPutBufferSize = 128
	// flushInterval bounds writer goroutine latency: even with one
	// straggler in the channel we batch+commit at least this often.
	flushInterval = 100 * time.Millisecond
	// flushBatch is the inner batch size — at most this many puts go
	// into a single bbolt batch transaction.
	flushBatch = 64
	// bodyFlushBatch is smaller than flushBatch because each body
	// write is up to bodyMaxBytes — keeping per-tx work bounded keeps
	// the writer responsive.
	bodyFlushBatch = 16
)

type metaPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Encoding      string `json:"encoding"`
}

type boltIndex struct {
	db    *bbolt.DB
	puts  chan putReq
	bputs chan bodyPutReq
	wg    sync.WaitGroup
	once  sync.Once
	stats memoryStats

	bodyCap         int64        // configured body-cache size cap (bytes); <=0 means no cap
	bodyCacheOff    bool         // when true, PutBody is a no-op and LookupBody always misses
	bodiesTotalSize atomic.Int64 // running total of encoded bodies bucket bytes (key+value), mirrored to meta on each batch
}

type putReq struct {
	key string
	val []byte
}

// bodyPutReq is the body-puts variant. Op distinguishes a Put (write
// new/updated body) from a Touch (only update the access timestamp,
// no body re-encode). Touch is the cheap LRU "I read this" signal so
// the body itself doesn't need to round-trip through gob on every read.
type bodyPutReq struct {
	key string
	val []byte // nil for Touch
	ts  int64  // AccessedUnixNano
}

func openBoltIndex(path string, cap BodyCacheCap) (*boltIndex, error) {
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

	maxBytes := cap.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultBodyCacheMaxBytes
	}
	idx := &boltIndex{
		db:           db,
		puts:         make(chan putReq, putBufferSize),
		bputs:        make(chan bodyPutReq, bodyPutBufferSize),
		bodyCap:      maxBytes,
		bodyCacheOff: cap.Disable,
	}

	// Recover the running bodies-total-size counter from meta so cap
	// enforcement carries across restarts. Fall through to 0 on any
	// read error — the next batch flush will reconcile.
	_ = db.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(bucketMeta))
		if meta == nil {
			return nil
		}
		raw := meta.Get([]byte(metaBodiesSizeKey))
		if len(raw) != 8 {
			return nil
		}
		idx.bodiesTotalSize.Store(int64(binary.BigEndian.Uint64(raw)))
		return nil
	})

	idx.wg.Add(2)
	go idx.writerLoop()
	go idx.bodyWriterLoop()
	return idx, nil
}

func initOrValidateSchema(db *bbolt.DB) error {
	return db.Update(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(bucketMeta))
		if meta == nil {
			// Fresh file: create the meta + data buckets and stamp
			// schema metadata.
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
			if _, err := tx.CreateBucket([]byte(bucketBodies)); err != nil {
				return fmt.Errorf("create bodies bucket: %w", err)
			}
			if _, err := tx.CreateBucket([]byte(bucketBodyAccess)); err != nil {
				return fmt.Errorf("create body-access bucket: %w", err)
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
		// Schema-additive: bodies_v1 and body_access_v1 are absent from
		// pre-body-cache v1 files. Create them on first open of the
		// new binary so existing v1 indexes upgrade transparently
		// without an ErrSchemaMismatch.
		if tx.Bucket([]byte(bucketBodies)) == nil {
			if _, err := tx.CreateBucket([]byte(bucketBodies)); err != nil {
				return fmt.Errorf("create bodies bucket: %w", err)
			}
		}
		if tx.Bucket([]byte(bucketBodyAccess)) == nil {
			if _, err := tx.CreateBucket([]byte(bucketBodyAccess)); err != nil {
				return fmt.Errorf("create body-access bucket: %w", err)
			}
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

// LookupBody returns the cached body for absPath when (size, mtime)
// match. On hit it enqueues a Touch on the body-puts channel so the
// access timestamp updates lazily — the read path itself never blocks
// on a write.
func (b *boltIndex) LookupBody(absPath string, size int64, mtime time.Time) (string, bool) {
	if b.bodyCacheOff {
		b.stats.bodyMisses.Add(1)
		return "", false
	}
	if absPath == "" || mtime.IsZero() || !filepath.IsAbs(absPath) {
		b.stats.bodyMisses.Add(1)
		return "", false
	}
	var be *BodyEntry
	err := b.db.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(bucketBodies))
		if bk == nil {
			return nil
		}
		raw := bk.Get([]byte(absPath))
		if raw == nil {
			return nil
		}
		decoded, decErr := decodeBody(raw)
		if decErr != nil {
			return decErr
		}
		be = decoded
		return nil
	})
	if err != nil {
		b.stats.bodyErrors.Add(1)
		return "", false
	}
	if be == nil {
		b.stats.bodyMisses.Add(1)
		return "", false
	}
	if be.Size != size || be.ModTimeUnixNano != mtime.UnixNano() {
		b.stats.bodyStales.Add(1)
		return "", false
	}
	// Touch — non-blocking enqueue. Drop the touch if the channel is
	// full; LRU is best-effort and a missed touch just means the entry
	// looks slightly older than reality to eviction.
	select {
	case b.bputs <- bodyPutReq{key: absPath, val: nil, ts: time.Now().UnixNano()}:
	default:
	}
	b.stats.bodyHits.Add(1)
	return be.Body, true
}

// PutBody stores an extracted body for absPath. The encoded body must
// fit in bodyMaxBytes (8 MiB); larger bodies bump BodyOversize and are
// dropped. Non-blocking — the body-puts channel back-pressures into
// BodyErrors when full.
func (b *boltIndex) PutBody(absPath string, be *BodyEntry) error {
	if b.bodyCacheOff {
		return nil
	}
	if absPath == "" || be == nil || !filepath.IsAbs(absPath) {
		b.stats.bodyErrors.Add(1)
		return nil
	}
	val, err := encodeBody(be)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			b.stats.bodyOversize.Add(1)
		} else {
			b.stats.bodyErrors.Add(1)
		}
		return nil
	}
	select {
	case b.bputs <- bodyPutReq{key: absPath, val: val, ts: time.Now().UnixNano()}:
	default:
		b.stats.bodyErrors.Add(1)
	}
	return nil
}

func (b *boltIndex) Stats() Stats {
	s := Stats{
		Hits:                 b.stats.hits.Load(),
		Misses:               b.stats.misses.Load(),
		Puts:                 b.stats.puts.Load(),
		Stales:               b.stats.stales.Load(),
		Errors:               b.stats.errors.Load(),
		BodyHits:             b.stats.bodyHits.Load(),
		BodyMisses:           b.stats.bodyMisses.Load(),
		BodyPuts:             b.stats.bodyPuts.Load(),
		BodyStales:           b.stats.bodyStales.Load(),
		BodyEvictions:        b.stats.bodyEvictions.Load(),
		BodyOversize:         b.stats.bodyOversize.Load(),
		BodyErrors:           b.stats.bodyErrors.Load(),
		EmbedHits:            b.stats.embedHits.Load(),
		EmbedMisses:          b.stats.embedMisses.Load(),
		EmbedPuts:            b.stats.embedPuts.Load(),
		EmbedErrors:          b.stats.embedErrors.Load(),
		EmbedModelMismatches: b.stats.embedModelMismatches.Load(),
		BodiesTotalBytes:     b.bodiesTotalSize.Load(),
	}
	// Per-bucket entry counts come from bbolt's bucket.Stats(). Cheap
	// (no full scan; bbolt maintains running counts) but it's a View
	// transaction so we tolerate failure gracefully — counts default
	// to zero rather than failing the whole snapshot.
	_ = b.db.View(func(tx *bbolt.Tx) error {
		if bkt := tx.Bucket([]byte(bucketAttrs)); bkt != nil {
			s.AttrEntriesCount = uint64(bkt.Stats().KeyN)
		}
		if bkt := tx.Bucket([]byte(bucketBodies)); bkt != nil {
			s.BodyEntriesCount = uint64(bkt.Stats().KeyN)
		}
		return nil
	})
	return s
}

func (b *boltIndex) BumpEmbedStat(kind string) { bumpEmbedStat(&b.stats, kind) }

// ListAttrs walks attrs_v1 with a Cursor, collecting summaries for
// entries whose key contains substr. The iteration is sorted by key
// because bbolt keys are returned in lexicographic order — no extra
// sort step needed.
//
// Live (size, mtime) is stat'd per row to compute Stale; a missing
// file (os.Stat error) is reported as Stale=true rather than
// dropped. Pagination is offset + limit on the FILTERED slice.
func (b *boltIndex) ListAttrs(substr string, limit, offset int) ([]EntrySummary, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var out []EntrySummary
	var total int
	var matchPaths []string
	if err := b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketAttrs))
		if bkt == nil {
			return nil
		}
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if substr == "" || strings.Contains(string(k), substr) {
				total++
				matchPaths = append(matchPaths, string(k))
			}
		}
		// Slice for pagination.
		from := min(offset, len(matchPaths))
		to := min(from+limit, len(matchPaths))
		for _, p := range matchPaths[from:to] {
			raw := bkt.Get([]byte(p))
			if raw == nil {
				continue
			}
			e, err := decodeEntry(raw)
			if err != nil {
				continue
			}
			out = append(out, EntrySummary{
				Path:        p,
				ContentType: e.ContentType,
				Size:        e.Size,
				ModTime:     time.Unix(0, e.ModTimeUnixNano),
				Stale:       isAttrStaleEntry(p, e),
			})
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListBodies mirrors ListAttrs against bodies_v1 + body_access_v1.
func (b *boltIndex) ListBodies(substr string, limit, offset int) ([]BodySummary, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var out []BodySummary
	var total int
	var matchPaths []string
	if err := b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketBodies))
		if bkt == nil {
			return nil
		}
		access := tx.Bucket([]byte(bucketBodyAccess))
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if substr == "" || strings.Contains(string(k), substr) {
				total++
				matchPaths = append(matchPaths, string(k))
			}
		}
		from := min(offset, len(matchPaths))
		to := min(from+limit, len(matchPaths))
		for _, p := range matchPaths[from:to] {
			raw := bkt.Get([]byte(p))
			if raw == nil {
				continue
			}
			be, err := decodeBody(raw)
			if err != nil {
				continue
			}
			var lastAccess time.Time
			if access != nil {
				if av := access.Get([]byte(p)); len(av) == 8 {
					lastAccess = time.Unix(0, int64(binary.BigEndian.Uint64(av)))
				}
			}
			out = append(out, BodySummary{
				Path:       p,
				Size:       be.Size,
				ModTime:    time.Unix(0, be.ModTimeUnixNano),
				LastAccess: lastAccess,
				Stale:      isBodyStaleEntry(p, be),
			})
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// PeekAttrs returns the cached Entry for absPath bypassing the
// (size, mtime) validation that Lookup enforces. Used by the
// dashboard's detail view so stale entries can be displayed with a
// stale flag rather than hidden.
func (b *boltIndex) PeekAttrs(absPath string) (*Entry, bool) {
	var out *Entry
	_ = b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketAttrs))
		if bkt == nil {
			return nil
		}
		raw := bkt.Get([]byte(absPath))
		if raw == nil {
			return nil
		}
		e, err := decodeEntry(raw)
		if err != nil {
			return nil
		}
		out = e
		return nil
	})
	return out, out != nil
}

// PeekBody returns the cached BodyEntry for absPath bypassing
// validation. Same semantics as PeekAttrs.
func (b *boltIndex) PeekBody(absPath string) (*BodyEntry, bool) {
	var out *BodyEntry
	_ = b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketBodies))
		if bkt == nil {
			return nil
		}
		raw := bkt.Get([]byte(absPath))
		if raw == nil {
			return nil
		}
		be, err := decodeBody(raw)
		if err != nil {
			return nil
		}
		out = be
		return nil
	})
	return out, out != nil
}

// Delete removes the attribute + body + body-access entries for
// absPath in a single write transaction. Returns nil when none of
// the buckets contained the key — Delete is idempotent. Stats
// counters are NOT decremented (monotonic-since-process-start).
//
// Note on the body-cache running-total: PutBody's size accounting
// in bodiesTotalSize is approximate (it tracks puts, not deletes,
// because eviction's primary path is over-cap FIFO). Operator-
// initiated Delete decrements it for the affected entry too so the
// dashboard's "X MiB / 256 MiB used" doesn't drift permanently.
func (b *boltIndex) Delete(absPath string) error {
	if absPath == "" {
		return nil
	}
	key := []byte(absPath)
	return b.db.Update(func(tx *bbolt.Tx) error {
		if bodies := tx.Bucket([]byte(bucketBodies)); bodies != nil {
			if raw := bodies.Get(key); raw != nil {
				b.bodiesTotalSize.Add(-int64(len(key) + len(raw)))
			}
			_ = bodies.Delete(key)
		}
		if access := tx.Bucket([]byte(bucketBodyAccess)); access != nil {
			_ = access.Delete(key)
		}
		if attrs := tx.Bucket([]byte(bucketAttrs)); attrs != nil {
			_ = attrs.Delete(key)
		}
		return nil
	})
}

// Clear wipes every cached entry across attrs / bodies / body-access
// in a single write transaction. The three data buckets are deleted
// + re-created so subsequent Put / PutBody land in a fresh bucket.
// The meta bucket (carrying the schema version) is preserved.
//
// Stats counters stay monotonic-since-process-start (a "what's my
// hit-rate trend" question still answers across the clear).
func (b *boltIndex) Clear() error {
	err := b.db.Update(func(tx *bbolt.Tx) error {
		for _, name := range []string{bucketAttrs, bucketBodies, bucketBodyAccess} {
			if err := tx.DeleteBucket([]byte(name)); err != nil && !errors.Is(err, bolterrors.ErrBucketNotFound) {
				return err
			}
			if _, err := tx.CreateBucket([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		b.bodiesTotalSize.Store(0)
	}
	return err
}

// Close drains both writer channels, waits for the writer goroutines
// to exit, then closes the bbolt db. Safe to call once; subsequent
// calls are no-ops.
func (b *boltIndex) Close() error {
	var closeErr error
	b.once.Do(func() {
		close(b.puts)
		close(b.bputs)
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

// bodyWriterLoop is the single writer for the bodies bucket. Mirrors
// writerLoop's batching pattern, but each flush also:
//
//   - Updates the body-access bucket with the latest AccessedUnixNano
//     for every Put OR Touch in the batch (Touch = nil val).
//   - Updates the running bodies_total_size meta key when bodies are
//     inserted or replaced.
//   - Evicts oldest-by-access-time entries when the running total
//     exceeds bodyCap.
//
// Eviction runs in the same transaction as the flush, so the
// post-batch invariant holds: bodiesTotalSize ≤ bodyCap. The eviction
// pass is bounded by how many entries it needs to drop, not by the
// bucket size, so it's cheap when the workload isn't churning.
func (b *boltIndex) bodyWriterLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	pending := make([]bodyPutReq, 0, bodyFlushBatch)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		var puts uint64
		err := b.db.Update(func(tx *bbolt.Tx) error {
			bodies := tx.Bucket([]byte(bucketBodies))
			access := tx.Bucket([]byte(bucketBodyAccess))
			meta := tx.Bucket([]byte(bucketMeta))
			if bodies == nil || access == nil || meta == nil {
				return errors.New("body buckets missing")
			}
			tsBuf := make([]byte, 8)
			for _, p := range pending {
				key := []byte(p.key)
				if p.val != nil {
					// Put — replace any existing body, adjusting the
					// running total by (newSize - oldSize).
					old := bodies.Get(key)
					oldSize := int64(0)
					if old != nil {
						oldSize = int64(len(key)) + int64(len(old))
					}
					newSize := int64(len(key)) + int64(len(p.val))
					if err := bodies.Put(key, p.val); err != nil {
						return err
					}
					b.bodiesTotalSize.Add(newSize - oldSize)
					puts++
				}
				// Both Put and Touch update the access timestamp.
				binary.BigEndian.PutUint64(tsBuf, uint64(p.ts))
				if err := access.Put(key, append([]byte{}, tsBuf...)); err != nil {
					return err
				}
			}
			// Mirror bodiesTotalSize to meta so it survives restarts.
			totalBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(totalBuf, uint64(b.bodiesTotalSize.Load()))
			if err := meta.Put([]byte(metaBodiesSizeKey), totalBuf); err != nil {
				return err
			}
			// Evict if over cap. Eviction walks the access bucket in
			// ascending timestamp order, deleting from both buckets
			// until the running total is back under cap.
			if b.bodyCap > 0 && b.bodiesTotalSize.Load() > b.bodyCap {
				if err := b.evictBodiesLocked(tx); err != nil {
					return err
				}
				// Re-mirror total after eviction.
				binary.BigEndian.PutUint64(totalBuf, uint64(b.bodiesTotalSize.Load()))
				if err := meta.Put([]byte(metaBodiesSizeKey), totalBuf); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.stats.bodyErrors.Add(uint64(len(pending)))
		} else {
			b.stats.bodyPuts.Add(puts)
		}
		pending = pending[:0]
	}

	for {
		select {
		case req, ok := <-b.bputs:
			if !ok {
				flush()
				return
			}
			pending = append(pending, req)
			if len(pending) >= bodyFlushBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// evictBodiesLocked drops oldest-by-access-timestamp entries until
// bodiesTotalSize ≤ bodyCap. Must be called inside an Update tx —
// it mutates bodies + body_access buckets directly. Bumps
// BodyEvictions for each deleted entry.
//
// Algorithm: walk body_access in ascending order (small fixed-size
// 8-byte values), build a list sorted by timestamp, then pop oldest
// and delete from both buckets until under cap. The first walk is
// O(N) in the bucket size but the inner sort+delete loop terminates
// as soon as the cap is satisfied.
func (b *boltIndex) evictBodiesLocked(tx *bbolt.Tx) error {
	bodies := tx.Bucket([]byte(bucketBodies))
	access := tx.Bucket([]byte(bucketBodyAccess))
	if bodies == nil || access == nil {
		return errors.New("body buckets missing during eviction")
	}

	type accessEntry struct {
		key []byte
		ts  uint64
	}
	var all []accessEntry
	if err := access.ForEach(func(k, v []byte) error {
		if len(v) != 8 {
			return nil
		}
		all = append(all, accessEntry{
			key: append([]byte{}, k...),
			ts:  binary.BigEndian.Uint64(v),
		})
		return nil
	}); err != nil {
		return err
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ts < all[j].ts })

	evicted := uint64(0)
	for _, e := range all {
		if b.bodiesTotalSize.Load() <= b.bodyCap {
			break
		}
		bodyVal := bodies.Get(e.key)
		entrySize := int64(0)
		if bodyVal != nil {
			entrySize = int64(len(e.key)) + int64(len(bodyVal))
		}
		if err := bodies.Delete(e.key); err != nil {
			return err
		}
		if err := access.Delete(e.key); err != nil {
			return err
		}
		b.bodiesTotalSize.Add(-entrySize)
		evicted++
	}
	b.stats.bodyEvictions.Add(evicted)
	return nil
}

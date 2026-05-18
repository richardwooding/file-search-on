package hashset

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// BuildOpts tunes Build. Format selects how to interpret the input
// file: "text" for newline-separated hashes (mixed algorithms,
// auto-detected by length), "nsrl" for the NSRLFile.txt CSV format.
// "auto" (the default for the CLI) detects by inspecting the first
// non-blank line.
type BuildOpts struct {
	Format string // "text" / "nsrl" / "auto" (default "auto")
	// Progress, when non-nil, is invoked roughly every BatchSize
	// rows with the cumulative count read so far. Useful for
	// CLI progress reporting against multi-GB NSRL drops.
	Progress func(total int64)
	// BatchSize controls how many rows are buffered before each
	// bbolt commit. Bigger = fewer commits but bigger transactions.
	// 0 → DefaultBuildBatchSize.
	BatchSize int
}

// DefaultBuildBatchSize is the bbolt transaction batch size. 50k
// rows balances commit overhead against memory usage during the
// build pass.
const DefaultBuildBatchSize = 50_000

// Build reads from r and writes a bbolt hashset file at outPath.
// Existing files are overwritten.
//
// The output is a bbolt database with four buckets:
//
//   - md5 / sha1 / sha256 — raw 16/20/32-byte hash bytes as keys,
//     empty values (membership is the signal).
//   - meta — schema_version=1.
//
// Read via OpenBolt / Open. Single-threaded; the bottleneck on
// NSRL-scale input is fsync, not parse — running multiple builders
// in parallel doesn't help.
func Build(r io.Reader, outPath string, opts BuildOpts) error {
	abs, err := filepath.Abs(outPath)
	if err != nil {
		return fmt.Errorf("resolve out path: %w", err)
	}
	// Remove an existing file so we always start clean — partial
	// state from a botched previous build would otherwise persist.
	_ = os.Remove(abs)

	db, err := bbolt.Open(abs, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("create hashset: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Create buckets + schema marker up front.
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range []string{boltBucketMD5, boltBucketSHA1, boltBucketSHA256, boltBucketMeta} {
			if _, berr := tx.CreateBucketIfNotExists([]byte(bucket)); berr != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, berr)
			}
		}
		meta := tx.Bucket([]byte(boltBucketMeta))
		return meta.Put([]byte(boltSchemaKey), []byte(boltSchemaValue))
	}); err != nil {
		return err
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBuildBatchSize
	}

	format := opts.Format
	if format == "" || format == "auto" {
		// Auto-detect via peek at the first ~4 KiB. NSRL files
		// start with a header line that includes the literal
		// `"SHA-1","MD5"` token.
		br := bufio.NewReader(r)
		peek, _ := br.Peek(4096)
		if strings.Contains(string(peek), `"SHA-1"`) {
			format = "nsrl"
		} else {
			format = "text"
		}
		r = br
	}

	switch format {
	case "text":
		return buildFromText(db, r, batchSize, opts.Progress)
	case "nsrl":
		return buildFromNSRL(db, r, batchSize, opts.Progress)
	}
	return fmt.Errorf("unknown hashset format %q", format)
}

// hashInsert is a single (algorithm, raw-bytes) pair pending insert.
type hashInsert struct {
	algo string
	raw  []byte
}

func buildFromText(db *bbolt.DB, r io.Reader, batchSize int, progress func(int64)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var total int64
	batch := make([]hashInsert, 0, batchSize)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.ToLower(line)
		algo, ok := LineLengthToAlgo[len(line)]
		if !ok {
			return fmt.Errorf("line %d: %w (length %d)", lineNum, ErrUnknownAlgo, len(line))
		}
		raw, err := hex.DecodeString(line)
		if err != nil {
			return fmt.Errorf("line %d: %w: %v", lineNum, ErrInvalidHex, err)
		}
		batch = append(batch, hashInsert{algo: algo, raw: raw})
		if len(batch) >= batchSize {
			if err := flushBatch(db, batch); err != nil {
				return err
			}
			total += int64(len(batch))
			batch = batch[:0]
			if progress != nil {
				progress(total)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(batch) > 0 {
		if err := flushBatch(db, batch); err != nil {
			return err
		}
		total += int64(len(batch))
		if progress != nil {
			progress(total)
		}
	}
	return nil
}

// buildFromNSRL parses NSRLFile.txt (a quoted-CSV file with header).
// Columns 0 and 1 are SHA-1 and MD5; we extract both.
//
// NSRL Modern RDS file format (header row literal):
//
//	"SHA-1","MD5","CRC32","FileName","FileSize","ProductCode","OpSystemCode","SpecialCode"
//
// Some legacy / minimal drops omit columns or reorder them; we lean
// on the header row to remap. If the header doesn't include either
// "SHA-1" or "MD5", we return a clear error.
func buildFromNSRL(db *bbolt.DB, r io.Reader, batchSize int, progress func(int64)) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = true

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read NSRL header: %w", err)
	}
	sha1Col, md5Col := -1, -1
	for i, h := range header {
		switch strings.TrimSpace(h) {
		case "SHA-1", "SHA1":
			sha1Col = i
		case "MD5":
			md5Col = i
		}
	}
	if sha1Col < 0 && md5Col < 0 {
		return errors.New("NSRL header missing both SHA-1 and MD5 columns")
	}

	var total int64
	batch := make([]hashInsert, 0, batchSize)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read NSRL row: %w", err)
		}
		if sha1Col >= 0 && sha1Col < len(rec) {
			s := strings.ToLower(strings.TrimSpace(rec[sha1Col]))
			if len(s) == 40 {
				if raw, derr := hex.DecodeString(s); derr == nil {
					batch = append(batch, hashInsert{algo: boltBucketSHA1, raw: raw})
				}
			}
		}
		if md5Col >= 0 && md5Col < len(rec) {
			s := strings.ToLower(strings.TrimSpace(rec[md5Col]))
			if len(s) == 32 {
				if raw, derr := hex.DecodeString(s); derr == nil {
					batch = append(batch, hashInsert{algo: boltBucketMD5, raw: raw})
				}
			}
		}
		if len(batch) >= batchSize {
			if err := flushBatch(db, batch); err != nil {
				return err
			}
			total += int64(len(batch))
			batch = batch[:0]
			if progress != nil {
				progress(total)
			}
		}
	}
	if len(batch) > 0 {
		if err := flushBatch(db, batch); err != nil {
			return err
		}
		total += int64(len(batch))
		if progress != nil {
			progress(total)
		}
	}
	return nil
}

func flushBatch(db *bbolt.DB, batch []hashInsert) error {
	if len(batch) == 0 {
		return nil
	}
	return db.Update(func(tx *bbolt.Tx) error {
		for _, h := range batch {
			bk := tx.Bucket([]byte(h.algo))
			if bk == nil {
				return fmt.Errorf("bucket %s missing during build", h.algo)
			}
			if err := bk.Put(h.raw, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

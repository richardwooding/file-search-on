// Package cryptohash computes md5 + sha1 + sha256 of a file in a
// single pass. Lives in its own package so both `internal/celexpr`
// (the ComputeHashes path inside BuildAttributesWith) and
// `internal/search` (FindDuplicates) can depend on it without
// introducing an import cycle.
//
// Forensic interop is the primary motivation (NSRL ships MD5+SHA1;
// VirusTotal and most published IOCs use MD5 or SHA1; modern tools
// index by SHA256). Computing all three at once costs ~30% extra
// CPU over SHA256-alone — dwarfed by file I/O for any input larger
// than a few KB.
package cryptohash

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
)

// Trio carries the three hex-encoded digests computed in one pass.
// All three are lowercase hex strings of their canonical length
// (32 / 40 / 64 chars respectively). Empty when computation failed
// or hashing wasn't requested.
type Trio struct {
	MD5    string
	SHA1   string
	SHA256 string
}

// File streams path through md5, sha1, and sha256 in one pass.
// Uses io.MultiWriter for throughput; ctx is checked at entry and
// between chunks so cancellation propagates on multi-GB files. No
// size cap — callers decide whether to hash a file before calling
// in (FindDuplicates prunes unique-size files first; the
// ComputeHashes opt-in in BuildAttributesWith runs for every match
// the CEL filter returned).
func File(ctx context.Context, path string) (Trio, error) {
	if err := ctx.Err(); err != nil {
		return Trio{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Trio{}, err
	}
	defer func() { _ = f.Close() }()

	h5 := md5.New()
	h1 := sha1.New()
	h256 := sha256.New()
	mw := io.MultiWriter(h5, h1, h256)

	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return Trio{}, err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := mw.Write(buf[:n]); werr != nil {
				return Trio{}, werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return Trio{}, rerr
		}
	}

	return Trio{
		MD5:    encode(h5),
		SHA1:   encode(h1),
		SHA256: encode(h256),
	}, nil
}

func encode(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

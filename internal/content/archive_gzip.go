package content

import (
	"encoding/binary"
	"io"
	"io/fs"
)

// readGZIPArchive surfaces metadata for a standalone .gz file (a
// single compressed payload, NOT a tar.gz). Two pieces:
//
//   - entry_count = 1 — gzip carries exactly one stream per RFC 1952.
//   - uncompressed_size — read from the 4-byte ISIZE footer at the end
//     of the file. ISIZE is the original uncompressed size MOD 2^32,
//     so files whose uncompressed payload exceeds 4 GiB report a
//     wrapped value. Documented; matches `gzip -l` behaviour.
//
// We don't decompress to count — that's expensive for large files and
// the ISIZE shortcut is fast and 99%-accurate.
//
// top_level_entries: gzip stores the original filename in the FNAME
// field (when the FLG.FNAME bit is set). We don't currently surface it
// — the standalone gzip case is rare enough that the file's own .gz
// path is a sufficient identifier.
//
// has_root_dir is always false for standalone gzip (a single file, no
// directory structure).
func readGZIPArchive(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < 4 {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(size-4, io.SeekStart); err != nil {
		return Attributes{}, nil //nolint:nilerr // unreadable footer → empty attrs
	}
	var footer [4]byte
	if _, err := io.ReadFull(rs, footer[:]); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	uncompressed := int64(binary.LittleEndian.Uint32(footer[:]))
	return archiveAttrs(1, uncompressed, map[string]struct{}{}), nil
}

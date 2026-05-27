package content

import (
	"context"
	"encoding/binary"
	"io"
	"io/fs"
)

// SQLite WAL (write-ahead log) sidecar format per
// https://www.sqlite.org/walformat.html. A WAL file lives next to its
// parent SQLite database under the same basename + `-wal` suffix and
// holds pages that have been committed but not yet checkpointed back
// into the main DB. Detecting WAL files completes the SQLite trio:
// `.db` + `.db-wal` + `.db-shm` all surface as distinct content types
// during a tree walk.
//
// The header is a fixed 32 bytes followed by zero or more 24-byte
// frame headers + page-sized payloads. We parse the header only —
// frame contents would let us see uncommitted writes but adds enough
// complexity (per-frame checksums, page reconstruction) that it's
// scoped out per issue #176.

var (
	sqliteWALMagicBE = []byte{0x37, 0x7F, 0x06, 0x82}
	sqliteWALMagicLE = []byte{0x37, 0x7F, 0x06, 0x83}
)

const (
	sqliteWALHeaderLen   = 32
	sqliteWALFrameHeader = 24
)

func init() {
	Register(&sqliteWALType{})
}

// sqliteWALType registers the database/sqlite-wal content type.
// `sqlite_format_version == 2` (WAL mode) ships these sidecars by the
// hundreds under ~/Library, ~/.config, and app-data trees — every
// modern Apple / Chrome / Firefox database touches them.
type sqliteWALType struct{}

func (s *sqliteWALType) Name() string { return "database/sqlite-wal" }
func (s *sqliteWALType) Extensions() []string {
	return []string{".db-wal", ".sqlite-wal", ".sqlite3-wal"}
}
func (s *sqliteWALType) MagicBytes() [][]byte {
	return [][]byte{sqliteWALMagicBE, sqliteWALMagicLE}
}

// Attributes reads the 32-byte WAL header and surfaces format /
// page-size / byte-order plus a best-effort frame count derived from
// the file's size. The frame count is approximate when the WAL has
// been recently checkpointed (trailing pages may be stale).
func (s *sqliteWALType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readSQLiteWALInfo(fsys, path)
}

func readSQLiteWALInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return Attributes{}, nil
	}
	buf := make([]byte, sqliteWALHeaderLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return Attributes{}, nil
	}
	return parseSQLiteWALHeader(buf[:n], info.Size()), nil
}

// parseSQLiteWALHeader walks the 32-byte WAL header. Pure function —
// fuzz target exercises it directly. Returns empty attrs on magic
// mismatch or short input.
//
// Layout per https://www.sqlite.org/walformat.html:
//
//	0-3    Magic 0x377F0682 (BE checksums) / 0x377F0683 (LE checksums)
//	4-7    File format version (BE u32, currently 3007000)
//	8-11   Database page size (BE u32)
//	12-15  Checkpoint sequence number (BE u32)
//	16-19  Salt-1 (random per checkpoint)
//	20-23  Salt-2
//	24-27  Checksum-1 of bytes 0-23
//	28-31  Checksum-2
//
// All multi-byte fields are big-endian regardless of the magic's
// trailing byte — only the per-frame checksum byte order varies.
func parseSQLiteWALHeader(data []byte, fileSize int64) Attributes {
	if len(data) < sqliteWALHeaderLen {
		return Attributes{}
	}
	var byteOrder string
	switch {
	case data[0] == sqliteWALMagicBE[0] && data[1] == sqliteWALMagicBE[1] &&
		data[2] == sqliteWALMagicBE[2] && data[3] == sqliteWALMagicBE[3]:
		byteOrder = "be"
	case data[0] == sqliteWALMagicLE[0] && data[1] == sqliteWALMagicLE[1] &&
		data[2] == sqliteWALMagicLE[2] && data[3] == sqliteWALMagicLE[3]:
		byteOrder = "le"
	default:
		return Attributes{}
	}

	pageSize := int64(binary.BigEndian.Uint32(data[8:12]))
	extras := Attributes{
		"sqlite_wal_format_version": int64(binary.BigEndian.Uint32(data[4:8])),
		"sqlite_wal_page_size":      pageSize,
		"sqlite_wal_checkpoint_seq": int64(binary.BigEndian.Uint32(data[12:16])),
		"sqlite_wal_byte_order":     byteOrder,
	}
	if fc, ok := walFrameCount(fileSize, pageSize); ok {
		extras["sqlite_wal_frame_count"] = fc
	}
	return databaseAttrs("sqlite-wal", extras)
}

// walFrameCount estimates the number of frames in the WAL from the
// file size and the page size declared in the header. Returns
// (count, true) only when the page size is sensible AND the file size
// is big enough to contain at least the header. Out-of-range page
// sizes (header is adversarial / WAL was truncated) return false so
// the attribute is just absent rather than misleading.
func walFrameCount(fileSize, pageSize int64) (int64, bool) {
	if fileSize < sqliteWALHeaderLen {
		return 0, false
	}
	// SQLite page sizes are powers of two from 512..65536. Reject
	// anything else — a header claiming page_size=1 would otherwise
	// produce a fantastically large frame count.
	if pageSize < 512 || pageSize > 65536 {
		return 0, false
	}
	if pageSize&(pageSize-1) != 0 {
		return 0, false
	}
	body := fileSize - sqliteWALHeaderLen
	frameSize := sqliteWALFrameHeader + pageSize
	return body / frameSize, true
}

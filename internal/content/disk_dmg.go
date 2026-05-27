package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
)

// dmgTrailerSize is the fixed size of the UDIF koly trailer at EOF.
const dmgTrailerSize = 512

// readDMGInfo parses the 512-byte UDIF (Universal Disk Image Format)
// koly trailer at the end of an Apple Disk Image. Detection has
// already happened by `.dmg` extension match (the koly magic at EOF
// can't be reached by the start-of-file sniffer).
//
// Trailer layout (big-endian throughout, per the Apple developer docs):
//
//	0x000 [4]    Signature "koly"
//	0x004 [4]    Version (current = 4)
//	0x008 [4]    HeaderSize (= 512)
//	0x00C [4]    Flags
//	0x010 [8]    RunningDataForkOffset
//	0x018 [8]    DataForkOffset
//	0x020 [8]    DataForkLength       (compressed payload size in file)
//	0x028 [8]    RsrcForkOffset
//	0x030 [8]    RsrcForkLength
//	... checksums, xml plist, etc ...
//	0x1EC [8]    SectorCount          (logical disk size in 512-byte sectors)
//
// virtual_size = SectorCount × 512 (the size the image presents when
// mounted, not the compressed file size). For images without a sector
// count (rare; UDIF format pre-2010), we fall back to 0.
func readDMGInfo(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < dmgTrailerSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(size-dmgTrailerSize, io.SeekStart); err != nil {
		return Attributes{}, nil
	}
	var trailer [dmgTrailerSize]byte
	if _, err := io.ReadFull(rs, trailer[:]); err != nil {
		return Attributes{}, nil
	}
	return parseDMGTrailer(trailer[:]), nil
}

// parseDMGTrailer is split out so the fuzz target can drive it with
// arbitrary 512-byte inputs without dealing with the filesystem.
// Returns empty attrs (not an error) when the input doesn't match the
// expected koly signature — the contract is "broken trailer → empty
// attrs, walker keeps moving".
func parseDMGTrailer(trailer []byte) Attributes {
	if len(trailer) < dmgTrailerSize {
		return Attributes{}
	}
	if !bytes.Equal(trailer[0:4], []byte("koly")) {
		return Attributes{}
	}
	// SectorCount at 0x1EC, big-endian uint64.
	sectorCount := binary.BigEndian.Uint64(trailer[0x1EC : 0x1EC+8])
	virtualSize := max(int64(sectorCount)*512,
		// Overflow guard for adversarial sector counts close to 2^63.
		0)
	return diskImageAttrs("udif", virtualSize, nil)
}

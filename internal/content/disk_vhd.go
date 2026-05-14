package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
	"time"
)

// vhdFooterSize is the fixed Connectix VHD footer length at EOF.
const vhdFooterSize = 512

// vhdEpoch is the VHD timestamp epoch (2000-01-01T00:00:00Z, seconds).
var vhdEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// readVHDInfo parses the 512-byte Connectix VHD footer at EOF.
//
// Footer layout (big-endian throughout):
//
//	0x00 [8]    Cookie "conectix"
//	0x08 [4]    Features
//	0x0C [4]    FileFormatVersion
//	0x10 [8]    DataOffset (0xFFFFFFFFFFFFFFFF for fixed; offset of
//	             dynamic-disk header for dynamic / differencing)
//	0x18 [4]    TimeStamp (seconds since 2000-01-01 UTC)
//	0x1C [4]    CreatorApplication
//	0x20 [4]    CreatorVersion
//	0x24 [4]    CreatorHostOS
//	0x28 [8]    OriginalSize
//	0x30 [8]    CurrentSize          (virtual disk size)
//	0x38 [4]    DiskGeometry
//	0x3C [4]    DiskType (2=fixed, 3=dynamic, 4=differencing)
//	... checksum, UUID, savedState, reserved ...
//
// Surfaces virtual_size = CurrentSize, disk_image_format = "vhd-fixed"
// / "vhd-dynamic" / "vhd-differencing" (driven by DiskType), disk_type
// = "fixed" / "dynamic" / "differencing", and created_at from the
// TimeStamp field.
func readVHDInfo(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < vhdFooterSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(size-vhdFooterSize, io.SeekStart); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	var footer [vhdFooterSize]byte
	if _, err := io.ReadFull(rs, footer[:]); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseVHDFooter(footer[:]), nil
}

func parseVHDFooter(footer []byte) Attributes {
	if len(footer) < vhdFooterSize {
		return Attributes{}
	}
	if !bytes.Equal(footer[0:8], []byte("conectix")) {
		return Attributes{}
	}
	currentSize := max(int64(binary.BigEndian.Uint64(footer[0x30:0x30+8])), 0)
	diskTypeByte := binary.BigEndian.Uint32(footer[0x3C : 0x3C+4])
	var formatStr, diskTypeStr string
	switch diskTypeByte {
	case 2:
		formatStr, diskTypeStr = "vhd-fixed", "fixed"
	case 3:
		formatStr, diskTypeStr = "vhd-dynamic", "dynamic"
	case 4:
		formatStr, diskTypeStr = "vhd-differencing", "differencing"
	default:
		// Unknown disk type — still surface the family but tag it.
		formatStr, diskTypeStr = "vhd-unknown", "unknown"
	}

	extras := Attributes{"disk_type": diskTypeStr}
	timestampSecs := binary.BigEndian.Uint32(footer[0x18 : 0x18+4])
	if timestampSecs > 0 {
		extras["created_at"] = vhdEpoch.Add(time.Duration(timestampSecs) * time.Second)
	}
	return diskImageAttrs(formatStr, currentSize, extras)
}

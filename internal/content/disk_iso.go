package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

// ISO 9660 primary volume descriptor (PVD) lives at logical block 16,
// which (at the standard 2048-byte block size) is byte offset 0x8000.
const (
	isoPVDOffset    = 0x8000
	isoPVDSize      = 2048
	isoSectorSize   = 2048
	isoVolDescPVD   = 0x01
	isoVolDescTerm  = 0xFF
	isoCD001Sig     = "CD001"
	isoVolLabelLen  = 32
	isoVolLabelOff  = 40 // System identifier (32) + 1+5+1+1 header
	isoVolSpaceOff  = 80 // both-endian uint32: LSB form at [80..84]
	isoCreationOff  = 813
	isoCreationSize = 17
)

// readISO9660Info parses the Primary Volume Descriptor of an ISO 9660
// disk image. The PVD lives at offset 0x8000 (16 × 2048-byte sector
// system area) and identifies itself with the literal "CD001" at
// relative offset 1.
//
// Surfaces:
//   - virtual_size = volume_space_size (LE uint32) × 2048-byte sectors
//   - volume_label = volume_identifier (32 bytes, space-padded d-chars)
//   - created_at   = volume_creation_date_and_time (17 bytes,
//     "YYYYMMDDHHMMSScc" + signed tz-offset byte in 15-min units)
//
// All fields decode best-effort: a malformed PVD returns empty attrs +
// nil err so the walker keeps going. Hybrid ISO/Joliet/UDF images use
// the same PVD shape; we only read the ISO 9660 PVD.
func readISO9660Info(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < isoPVDOffset+isoPVDSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(isoPVDOffset, io.SeekStart); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	var pvd [isoPVDSize]byte
	if _, err := io.ReadFull(rs, pvd[:]); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseISOPVD(pvd[:]), nil
}

// parseISOPVD is the pure-function half of readISO9660Info — easier to
// test and fuzz with arbitrary 2048-byte sectors.
func parseISOPVD(pvd []byte) Attributes {
	if len(pvd) < isoPVDSize {
		return Attributes{}
	}
	if pvd[0] != isoVolDescPVD {
		return Attributes{}
	}
	if !bytes.Equal(pvd[1:6], []byte(isoCD001Sig)) {
		return Attributes{}
	}

	// volume_space_size: ISO stores it both-endian, with the LE form
	// in the first 4 bytes and BE in the next 4. We trust LE.
	volumeSpaceSize := binary.LittleEndian.Uint32(pvd[isoVolSpaceOff : isoVolSpaceOff+4])
	virtualSize := max(int64(volumeSpaceSize)*isoSectorSize, 0)

	extras := Attributes{}

	label := strings.TrimRight(string(pvd[isoVolLabelOff:isoVolLabelOff+isoVolLabelLen]), " \x00")
	if label != "" {
		extras["volume_label"] = label
	}

	if t := parseISO9660Date(pvd[isoCreationOff : isoCreationOff+isoCreationSize]); !t.IsZero() {
		extras["created_at"] = t
	}

	return diskImageAttrs("iso9660", virtualSize, extras)
}

// parseISO9660Date decodes the 17-byte ISO 9660 "decimal" datetime
// format: "YYYYMMDDHHMMSScc" + signed tz-offset byte in 15-minute
// units east of GMT. The "all zeros / all spaces" form means "not
// specified" — we return the zero time so callers can detect that.
func parseISO9660Date(b []byte) time.Time {
	if len(b) < 17 {
		return zeroTime
	}
	s := string(b[:16])
	if strings.TrimSpace(s) == "" || strings.Trim(s, "0") == "" {
		return zeroTime
	}
	year, errY := strconv.Atoi(s[0:4])
	month, errMo := strconv.Atoi(s[4:6])
	day, errD := strconv.Atoi(s[6:8])
	hour, errH := strconv.Atoi(s[8:10])
	minute, errMi := strconv.Atoi(s[10:12])
	second, errS := strconv.Atoi(s[12:14])
	// b[14:16] is hundredths-of-a-second; we drop it — Go time has no
	// finer resolution than nanoseconds and we don't need < 1s here.
	if errY != nil || errMo != nil || errD != nil ||
		errH != nil || errMi != nil || errS != nil {
		return zeroTime
	}
	if year == 0 || month == 0 || day == 0 {
		return zeroTime
	}
	tzOffset := int(int8(b[16])) * 15 * 60 // 15-minute units → seconds
	return time.Date(year, time.Month(month), day, hour, minute, second, 0,
		time.FixedZone("", tzOffset))
}

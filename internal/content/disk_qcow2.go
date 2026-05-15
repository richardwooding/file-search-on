package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
)

// qcow2HeaderSize is the minimum (v2) QCOW2 header length read at
// offset 0. v3 extends the header but the v2 fields stay at the same
// offsets; we only need the first 0x48 bytes for the surfaces we
// expose.
const qcow2HeaderSize = 72

// readQCOW2Info parses the QCOW2 header at offset 0 of a QEMU
// Copy-On-Write v2/v3 image.
//
// Header layout (big-endian — QCOW2 is the BE outlier among the
// disk-image formats):
//
//	0x00 [4]    Magic "QFI\xfb"
//	0x04 [4]    Version (2 or 3)
//	0x08 [8]    BackingFileOffset
//	0x10 [4]    BackingFileSize
//	0x14 [4]    ClusterBits
//	0x18 [8]    Size                  (virtual disk size in bytes)
//	0x20 [4]    CryptMethod           (0=none, 1=AES, 2=LUKS)
//	0x24 [4]    L1Size
//	0x28 [8]    L1TableOffset
//	0x30 [8]    RefcountTableOffset
//	0x38 [4]    RefcountTableClusters
//	0x3C [4]    NbSnapshots
//	0x40 [8]    SnapshotsOffset
//	(v3 adds incompatible_features, compatible_features,
//	 autoclear_features, refcount_order, header_length from 0x48)
//
// Surfaces virtual_size, cluster_bits (int), is_encrypted (bool —
// any non-zero crypt_method).
func readQCOW2Info(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < qcow2HeaderSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	var hdr [qcow2HeaderSize]byte
	if _, err := io.ReadFull(rs, hdr[:]); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseQCOW2Header(hdr[:]), nil
}

func parseQCOW2Header(hdr []byte) Attributes {
	if len(hdr) < qcow2HeaderSize {
		return Attributes{}
	}
	if !bytes.Equal(hdr[0:4], []byte{'Q', 'F', 'I', 0xFB}) {
		return Attributes{}
	}
	clusterBits := int64(binary.BigEndian.Uint32(hdr[0x14 : 0x14+4]))
	virtualSize := max(int64(binary.BigEndian.Uint64(hdr[0x18:0x18+8])), 0)
	cryptMethod := binary.BigEndian.Uint32(hdr[0x20 : 0x20+4])
	return diskImageAttrs("qcow2", virtualSize, Attributes{
		"cluster_bits": clusterBits,
		"is_encrypted": cryptMethod != 0,
	})
}

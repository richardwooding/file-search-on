package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
)

// vmdkSparseHeaderSize is the size of the SparseExtentHeader read at
// offset 0. The on-disk struct is 77 bytes; we read 80 to keep the
// field accesses aligned and bounds-friendly.
const vmdkSparseHeaderSize = 80

// readVMDKInfo parses the binary SparseExtentHeader at offset 0 of a
// VMware Virtual Disk sparse-extent file. Detection has already
// matched the "KDMV" magic via the start-of-file sniffer.
//
// Header layout (little-endian; the struct is packed 77 bytes):
//
//	0x00 [4]    magicNumber  = "KDMV"
//	0x04 [4]    version      (1, 2, or 3)
//	0x08 [4]    flags
//	0x0C [8]    capacity     (in 512-byte sectors)
//	0x14 [8]    grainSize    (in sectors)
//	0x1C [8]    descriptorOffset
//	0x24 [8]    descriptorSize
//	0x2C [4]    numGTEsPerGT
//	0x30 [8]    rgdOffset
//	0x38 [8]    gdOffset
//	0x40 [8]    overHead
//	... unclean-shutdown + end-of-line markers + compression algo ...
//
// VMDK has two on-disk forms — the binary sparse extent above, and a
// plain-text "Disk DescriptorFile" that references external extents.
// Only the sparse form gets here (the text form falls through to the
// `text` content type via extension+content sniffing).
//
// Surfaces virtual_size = capacity × 512, disk_type = "sparse"
// (or "sparse-compressed" when the compressed flag bit is set).
func readVMDKInfo(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < vmdkSparseHeaderSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return Attributes{}, nil
	}
	var hdr [vmdkSparseHeaderSize]byte
	if _, err := io.ReadFull(rs, hdr[:]); err != nil {
		return Attributes{}, nil
	}
	return parseVMDKSparseHeader(hdr[:]), nil
}

func parseVMDKSparseHeader(hdr []byte) Attributes {
	if len(hdr) < vmdkSparseHeaderSize {
		return Attributes{}
	}
	if !bytes.Equal(hdr[0:4], []byte("KDMV")) {
		return Attributes{}
	}
	flags := binary.LittleEndian.Uint32(hdr[0x08 : 0x08+4])
	capacity := int64(binary.LittleEndian.Uint64(hdr[0x0C : 0x0C+8]))
	virtualSize := capacity * 512
	if virtualSize < 0 || capacity < 0 {
		// Overflow guard for adversarial capacities near 2^54.
		virtualSize = 0
	}
	// Flag bit 0x10000 = compressed grain table; everything else is
	// still a sparse extent.
	diskType := "sparse"
	if flags&0x10000 != 0 {
		diskType = "sparse-compressed"
	}
	return diskImageAttrs("vmdk-sparse", virtualSize, Attributes{
		"disk_type": diskType,
	})
}

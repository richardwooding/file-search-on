package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
)

// VHDX on-disk layout constants (Microsoft VHDX spec v1.00).
const (
	vhdxRegionTable1Offset = 0x30000 // 192 KiB — primary region table
	vhdxRegionTable2Offset = 0x40000 // 256 KiB — secondary copy
	vhdxRegionTableSize    = 64 * 1024
	vhdxMaxRegionEntries   = 2047

	vhdxMetadataTableHeaderSize = 32
	vhdxMaxMetadataEntries      = 2047
)

// vhdxMetadataRegionGUID is the GUID of the Metadata region inside the
// region-table entries. Stored on-disk in the standard "mixed-endian"
// GUID byte order (first three fields little-endian; final two big).
//
//	{8B7CA206-4790-4B9A-B8FE-575F050F886E}
var vhdxMetadataRegionGUID = []byte{
	0x06, 0xA2, 0x7C, 0x8B, // Data1 (LE uint32)
	0x90, 0x47, // Data2 (LE uint16)
	0x9A, 0x4B, // Data3 (LE uint16)
	0xB8, 0xFE, 0x57, 0x5F, 0x05, 0x0F, 0x88, 0x6E, // Data4 (BE 8 bytes)
}

// vhdxVirtualDiskSizeGUID identifies the "Virtual Disk Size" item
// inside the Metadata region's table.
//
//	{2FA54224-CD1B-4876-B211-5DBED83BF4B8}
var vhdxVirtualDiskSizeGUID = []byte{
	0x24, 0x42, 0xA5, 0x2F,
	0x1B, 0xCD,
	0x76, 0x48,
	0xB2, 0x11, 0x5D, 0xBE, 0xD8, 0x3B, 0xF4, 0xB8,
}

// readVHDXInfo parses a Microsoft Hyper-V VHDX disk image. Detection
// has already matched the "vhdxfile" magic at offset 0.
//
// VHDX is a complex layered format — File Identifier (64 KiB), two
// headers (atomic-update doubled), two region tables (doubled), then
// region payloads (BAT, Metadata, etc.). We walk:
//
//  1. Region Table 1 at 0x30000 to locate the Metadata region.
//  2. The Metadata region's table to find the Virtual Disk Size item.
//  3. The 8-byte VirtualDiskSize value at the item's offset.
//
// Best-effort throughout: any failure (truncated file, corrupt region
// table, missing metadata entry) surfaces disk_image_format = "vhdx"
// + virtual_size = 0. The walker keeps going.
//
// Region Table 2 at 0x40000 is the redundant copy; we don't currently
// fall back to it. Adding the fallback is straightforward if real
// corpora demand it.
func readVHDXInfo(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	// Minimum size to read the magic + try Region Table 1.
	if size < 8 {
		return Attributes{}, nil
	}
	// Confirm magic — detection matched against the registry but a
	// stray .vhdx file might be lying. parseVHDXVirtualSize itself is
	// defensive but the magic check makes the failure path cheaper.
	var magic [8]byte
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	if _, err := io.ReadFull(rs, magic[:]); err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	if !bytes.Equal(magic[:], []byte("vhdxfile")) {
		return Attributes{}, nil
	}

	virtualSize := readVHDXVirtualSize(rs, size)
	return diskImageAttrs("vhdx", virtualSize, nil), nil
}

// readVHDXVirtualSize walks the VHDX region-table chain to extract
// VirtualDiskSize. Returns 0 if any step fails. Split out from
// readVHDXInfo so the fuzz target can drive the walk with arbitrary
// byte arrays via a bytes.Reader.
func readVHDXVirtualSize(rs io.ReadSeeker, size int64) int64 {
	if size < vhdxRegionTable1Offset+vhdxRegionTableSize {
		return 0
	}

	if _, err := rs.Seek(vhdxRegionTable1Offset, io.SeekStart); err != nil {
		return 0
	}
	rt := make([]byte, vhdxRegionTableSize)
	if _, err := io.ReadFull(rs, rt); err != nil {
		return 0
	}
	metaOffset, metaLength := findVHDXMetadataRegion(rt)
	if metaLength == 0 {
		return 0
	}
	// Bounds-check the metadata region against the file size before
	// we trust the offset.
	if metaOffset < 0 || metaOffset+int64(metaLength) > size {
		return 0
	}

	if _, err := rs.Seek(metaOffset, io.SeekStart); err != nil {
		return 0
	}
	// Cap the read at the metadata region length AND a sane absolute
	// ceiling (16 MiB — VHDX metadata regions are typically 1 MiB).
	readLen := int64(metaLength)
	const metadataReadCap = 16 * 1024 * 1024
	if readLen > metadataReadCap {
		readLen = metadataReadCap
	}
	meta := make([]byte, readLen)
	if _, err := io.ReadFull(rs, meta); err != nil {
		return 0
	}
	return findVHDXVirtualDiskSize(meta)
}

// findVHDXMetadataRegion parses a VHDX region table and returns the
// (FileOffset, Length) of the Metadata region. Returns (0, 0) if not
// found or if the table is malformed.
//
// Region Table layout (64 KiB block, all little-endian):
//
//	0x00 [4]    Signature "regi"
//	0x04 [4]    Checksum
//	0x08 [4]    EntryCount (max 2047)
//	0x0C [4]    Reserved
//	0x10..      RegionTableEntry[] (32 bytes each):
//	             [16] Guid
//	             [8]  FileOffset
//	             [4]  Length
//	             [4]  Required (bit 0)
func findVHDXMetadataRegion(rt []byte) (int64, uint32) {
	if len(rt) < 16 {
		return 0, 0
	}
	if !bytes.Equal(rt[0:4], []byte("regi")) {
		return 0, 0
	}
	entryCount := binary.LittleEndian.Uint32(rt[0x08 : 0x08+4])
	if entryCount > vhdxMaxRegionEntries {
		// Reject pathological tables claiming more entries than the
		// format allows.
		return 0, 0
	}
	for i := range entryCount {
		off := 16 + int(i)*32
		if off+32 > len(rt) {
			return 0, 0
		}
		entry := rt[off : off+32]
		if !bytes.Equal(entry[0:16], vhdxMetadataRegionGUID) {
			continue
		}
		fileOffset := int64(binary.LittleEndian.Uint64(entry[16 : 16+8]))
		length := binary.LittleEndian.Uint32(entry[24 : 24+4])
		if fileOffset < 0 {
			return 0, 0
		}
		return fileOffset, length
	}
	return 0, 0
}

// findVHDXVirtualDiskSize parses a VHDX Metadata region and returns
// the VirtualDiskSize value (in bytes), or 0 if not found.
//
// Metadata region layout (all little-endian):
//
//	0x00 [8]    Signature "metadata"
//	0x08 [2]    Reserved
//	0x0A [2]    EntryCount
//	0x0C [20]   Reserved
//	0x20..      MetadataTableEntry[] (32 bytes each):
//	             [16] ItemId (GUID)
//	             [4]  Offset (from start of metadata region)
//	             [4]  Length
//	             [4]  Flags
//	             [4]  Reserved2
//
// The VirtualDiskSize item points to an 8-byte little-endian uint64
// value at the referenced offset.
func findVHDXVirtualDiskSize(meta []byte) int64 {
	if len(meta) < vhdxMetadataTableHeaderSize {
		return 0
	}
	if !bytes.Equal(meta[0:8], []byte("metadata")) {
		return 0
	}
	entryCount := binary.LittleEndian.Uint16(meta[0x0A : 0x0A+2])
	if entryCount > vhdxMaxMetadataEntries {
		return 0
	}
	for i := range entryCount {
		off := vhdxMetadataTableHeaderSize + int(i)*32
		if off+32 > len(meta) {
			return 0
		}
		entry := meta[off : off+32]
		if !bytes.Equal(entry[0:16], vhdxVirtualDiskSizeGUID) {
			continue
		}
		valueOffset := binary.LittleEndian.Uint32(entry[16 : 16+4])
		valueLength := binary.LittleEndian.Uint32(entry[20 : 20+4])
		if valueLength < 8 {
			return 0
		}
		end := int(valueOffset) + 8
		if int(valueOffset) > len(meta) || end > len(meta) {
			return 0
		}
		size := int64(binary.LittleEndian.Uint64(meta[valueOffset:end]))
		if size < 0 {
			return 0
		}
		return size
	}
	return 0
}

package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
)

// wimHeaderSize is the WIM_HEADER_V1 struct size. We read enough to
// reach dwImageCount at offset 0x2C — the trailing reservedTable
// fields don't drive any of our surfaces.
const wimHeaderSize = 48

// readWIMInfo parses the WIM_HEADER at offset 0 of a Windows Imaging
// Format file. Detection has already matched the "MSWIM\0\0\0" magic.
//
// Header layout (little-endian):
//
//	0x00 [8]    ImageTag = "MSWIM\0\0\0"
//	0x08 [4]    cbSize  (header size, typically 208)
//	0x0C [4]    dwVersion
//	0x10 [4]    dwFlags
//	0x14 [4]    dwCompressionSize
//	0x18 [16]   gWIMGuid
//	0x28 [2]    usPartNumber
//	0x2A [2]    usTotalParts
//	0x2C [4]    dwImageCount         (number of images in this archive)
//	... resource headers (rhOffsetTable, rhXmlData, rhBootMetadata) ...
//
// virtual_size is N/A for WIM (it's a file-level archive, not a disk
// image with a logical sector count) — we surface it as 0 and let
// agents read the always-on `size` attribute for on-disk footprint.
//
// Surfaces disk_image_format = "wim", image_count.
func readWIMInfo(fsys fs.FS, path string) (Attributes, error) {
	rs, size, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	if size < wimHeaderSize {
		return Attributes{}, nil
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return Attributes{}, nil
	}
	var hdr [wimHeaderSize]byte
	if _, err := io.ReadFull(rs, hdr[:]); err != nil {
		return Attributes{}, nil
	}
	return parseWIMHeader(hdr[:]), nil
}

func parseWIMHeader(hdr []byte) Attributes {
	if len(hdr) < wimHeaderSize {
		return Attributes{}
	}
	if !bytes.Equal(hdr[0:8], []byte{'M', 'S', 'W', 'I', 'M', 0, 0, 0}) {
		return Attributes{}
	}
	imageCount := int64(binary.LittleEndian.Uint32(hdr[0x2C : 0x2C+4]))
	return diskImageAttrs("wim", 0, Attributes{
		"image_count": imageCount,
	})
}

package content

import (
	"bytes"
	"io"
	"io/fs"
)

// xarHeaderSize is the standard XAR header length read at offset 0.
// The format actually declares its own size in the header (uint16 at
// offset 4, currently 28) but the prefix we need to identify the
// format is fixed.
const xarHeaderSize = 28

// readPKGInfo identifies a macOS installer package (XAR archive).
// Detection already matched the "xar!" magic at offset 0 — we just
// confirm and surface `package_format` + `package_kind`.
//
// Header layout (big-endian per Apple's XAR spec):
//
//	0x00 [4]    Magic "xar!"
//	0x04 [2]    HeaderSize  (uint16; typically 28)
//	0x06 [2]    Version     (uint16; currently 1)
//	0x08 [8]    TocLengthCompressed
//	0x10 [8]    TocLengthUncompressed
//	0x18 [4]    ChecksumAlg (0=none, 1=SHA-1, 2=MD5, 3=SHA-256, 4=SHA-512)
//
// The TOC following the header is a gzip-compressed XML document
// listing all entries with metadata + checksum + offset. Walking it
// would surface a meaningful entry_count but is out of scope for v1.
func readPKGInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var hdr [xarHeaderSize]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return Attributes{}, nil
	}
	if !bytes.Equal(hdr[0:4], []byte("xar!")) {
		return Attributes{}, nil
	}
	return installPackageAttrs("xar", Attributes{
		"package_kind": "macos-installer",
	}), nil
}

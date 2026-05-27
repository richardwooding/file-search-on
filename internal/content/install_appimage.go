package content

import (
	"bytes"
	"io"
	"io/fs"
)

// AppImage's distinguishing 4-byte marker lives at offset 8 (overlaid
// on the underlying ELF e_ident padding bytes 7-11 — bytes 7-9 are
// the ELF ABI version + padding, which AppImage repurposes). Two
// versions exist in the wild:
//
//	v1: 0x41 0x49 0x01 0x00  (rare — superseded by v2 in 2017)
//	v2: 0x41 0x49 0x02 0x00  (the current format)
const (
	appimageMagicOffset = 8
	appimageReadSize    = 16
)

var (
	appimageMagicV1 = []byte{'A', 'I', 0x01, 0x00}
	appimageMagicV2 = []byte{'A', 'I', 0x02, 0x00}
)

// readAppImageInfo identifies a Linux portable AppImage. The file is
// an ELF binary (the AppImage runtime) with a SquashFS image
// appended; AppImage v1/v2 stamp 4 distinguishing bytes at ELF offset
// 8 to mark themselves.
//
// Detection is by extension (.appimage) since the marker isn't at
// offset 0 — same pattern as TAR / DMG / VHD. The Attributes parser
// confirms the marker before claiming the file; a file named .appimage
// without the correct stamp surfaces as `install/appimage` content
// type but empty attrs (matches the "broken file doesn't fail the
// walk" pattern).
//
// Out of scope: walking the appended SquashFS to surface anything
// from the desktop entry (icon, name, version, categories). Reading
// SquashFS requires either a third-party library or hand-rolling a
// SquashFS reader — both larger than v1 should carry.
func readAppImageInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var hdr [appimageReadSize]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return Attributes{}, nil
	}
	marker := hdr[appimageMagicOffset : appimageMagicOffset+4]
	extras := Attributes{"package_kind": "linux-portable"}
	switch {
	case bytes.Equal(marker, appimageMagicV2):
		extras["appimage_version"] = int64(2)
	case bytes.Equal(marker, appimageMagicV1):
		extras["appimage_version"] = int64(1)
	default:
		// Marker doesn't match — file is named .appimage but isn't
		// one. Don't fabricate attributes.
		return Attributes{}, nil
	}
	return installPackageAttrs("appimage", extras), nil
}

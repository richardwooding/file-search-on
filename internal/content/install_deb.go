package content

import (
	"bytes"
	"io"
	"io/fs"
	"strings"
)

// debMagic is the Unix archive ("ar") magic that prefixes a Debian
// .deb package. The contained ar members are conventionally:
// `debian-binary`, `control.tar.{gz,xz,zst}`, `data.tar.{gz,xz,zst}`.
var debMagic = []byte("!<arch>\n")

// debHeaderReadSize is enough bytes to confirm the ar magic AND to
// peek at the first member's name (60-byte ar header). We don't need
// to read further for v1 — extracting control.tar.* to parse the
// Debian control file is a follow-up.
const debHeaderReadSize = 68

// readDEBInfo identifies a Debian binary package. Surfaces
// `package_format = "deb"` plus `package_kind = "binary"` (the
// alternative is `source` for the rare `.dsc` / `.changes` pair but
// those don't share the .deb extension).
//
// Future: walk the ar to find control.tar.* and extract the `control`
// file inside to surface package_name + package_version + arch. Out of
// scope for v1 because it'd require nesting an ar walk inside a tar
// walk inside a gzip/xz/zst decoder.
//
// ar header layout (60 bytes per member, ASCII text fields):
//
//	0x00 [16]   name (space-padded; trailing "/" terminator)
//	0x10 [12]   timestamp (decimal seconds since epoch)
//	0x1C [6]    owner uid
//	0x22 [6]    group gid
//	0x28 [8]    mode (octal)
//	0x30 [10]   size (decimal bytes)
//	0x3A [2]    end-of-header marker "`\n"
func readDEBInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var hdr [debHeaderReadSize]byte
	n, _ := io.ReadFull(f, hdr[:])
	if n < len(debMagic) {
		return Attributes{}, nil
	}
	if !bytes.HasPrefix(hdr[:n], debMagic) {
		return Attributes{}, nil
	}
	extras := Attributes{"package_kind": "binary"}
	// The first ar member after the magic should be `debian-binary`.
	// If it's something else, the file is probably not a Debian
	// package despite the extension (some Linux distros also use ar
	// for unrelated archives). Don't surface package_kind in that
	// case — but we still surface package_format = "deb" since the
	// magic matched.
	if n >= debHeaderReadSize {
		firstMemberName := strings.TrimRight(
			strings.TrimSpace(string(hdr[len(debMagic):len(debMagic)+16])),
			"/")
		if firstMemberName != "debian-binary" {
			delete(extras, "package_kind")
		}
	}
	return installPackageAttrs("deb", extras), nil
}

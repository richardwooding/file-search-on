package content

import (
	"context"
	"errors"
	"io/fs"
	"maps"
)

func init() {
	Register(&installPackageType{
		name:  "install/pkg",
		exts:  []string{".pkg"},
		magic: [][]byte{[]byte("xar!")},
	})
	Register(&installPackageType{
		name:  "install/deb",
		exts:  []string{".deb"},
		magic: [][]byte{[]byte("!<arch>\n")},
	})
	Register(&installPackageType{
		name:  "install/rpm",
		exts:  []string{".rpm"},
		magic: [][]byte{{0xED, 0xAB, 0xEE, 0xDB}},
	})
	Register(&installPackageType{
		name: "install/appimage",
		exts: []string{".appimage"},
		// AppImage's distinguishing 4 bytes live at offset 8 (overlaid
		// on the underlying ELF's e_ident padding), so the prefix
		// sniffer can't see it. Extension-only — same pattern as TAR
		// (offset-257 magic) and DMG (EOF trailer).
		magic: nil,
	})
}

type installPackageType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (p *installPackageType) Name() string         { return p.name }
func (p *installPackageType) Extensions() []string { return p.exts }
func (p *installPackageType) MagicBytes() [][]byte { return p.magic }

// Attributes dispatches to a per-format install-package parser. Each
// parser reads a fixed-offset header only — no payload walk, no
// extraction. The cross-format surface is `package_format` (always
// set) plus `package_kind` / `package_name` / `package_version` /
// `package_arch` where the format makes them cheap to read.
//
// Out of scope: extracting control.tar.gz inside .deb to read the
// Debian control file (would need two nested archive walks);
// parsing the RPM Header following the Lead (the Lead carries
// name+arch+kind which is enough for triage); walking the SquashFS
// inside .appimage.
func (p *installPackageType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch p.name {
	case "install/pkg":
		return readPKGInfo(fsys, path)
	case "install/deb":
		return readDEBInfo(fsys, path)
	case "install/rpm":
		return readRPMInfo(fsys, path)
	case "install/appimage":
		return readAppImageInfo(fsys, path)
	}
	return nil, errors.New("unsupported install-package type")
}

// installPackageAttrs packs the cross-format surface plus any extras
// into a content.Attributes map. `format` is always present; the rest
// (`package_name`, `package_version`, `package_arch`, `package_kind`)
// come through extras where the per-format parser can read them.
// Mirrors archiveAttrs / binaryAttrs / diskImageAttrs.
func installPackageAttrs(format string, extras Attributes) Attributes {
	out := Attributes{
		"package_format": format,
	}
	maps.Copy(out, extras)
	return out
}

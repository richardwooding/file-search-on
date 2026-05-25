package content

import (
	"context"
	"io"
	"io/fs"
)

// OpenType font (.otf). sfnt container with PostScript / Compact Font
// Format (CFF / CFF2) outlines. Magic at offset 0 is `OTTO` —
// distinguishes from TrueType-shaped sfnt (which uses 0x00010000).
//
// font_outline_kind discriminates further: `cff2` (modern variable-
// font-friendly CFF) vs `cff` (legacy CFF) vs the rare `truetype`
// (some "OTF" files actually carry TrueType glyf — the OTTO magic is
// formally OpenType-with-PostScript-outlines but the spec doesn't
// strictly enforce it).
//
// Issue #197.

func init() {
	Register(&otfType{})
}

type otfType struct{}

func (*otfType) Name() string         { return "font/otf" }
func (*otfType) Extensions() []string { return []string{".otf"} }
func (*otfType) MagicBytes() [][]byte { return [][]byte{sfntMagicOpenType} }

func (*otfType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, fontMaxBlobSize))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	info := parseSFNT(buf, 0)
	return sfntAttrs(info, "otf"), nil
}

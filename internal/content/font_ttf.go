package content

import (
	"context"
	"io"
	"io/fs"
)

// TrueType font (.ttf). The dominant font format on Apple platforms +
// Windows pre-OpenType. Bare sfnt container with TrueType outlines
// (`glyf` table). Magic at offset 0 is `0x00010000` (the "scaler
// version" field in the sfnt header — TrueType uses this version
// number for historical reasons; OTTO would be OpenType-with-CFF
// instead).
//
// Issue #197. parseSFNT does all the heavy lifting; this file is
// just the registration + format hand-off.

func init() {
	Register(&ttfType{})
}

type ttfType struct{}

func (*ttfType) Name() string         { return "font/ttf" }
func (*ttfType) Extensions() []string { return []string{".ttf"} }
func (*ttfType) MagicBytes() [][]byte { return [][]byte{sfntMagicTrueType, sfntMagicTrueLegacy} }

func (*ttfType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
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
	return sfntAttrs(info, "ttf"), nil
}

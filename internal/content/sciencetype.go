package content

import (
	"context"
	"io/fs"
	"maps"
)

func init() {
	Register(&fitsType{})
}

// fitsType registers the FITS (Flexible Image Transport System)
// content type — the dominant binary container in astronomy since
// 1981, used by every major observatory and space telescope. The
// header is ASCII keyword-value records inside 2880-byte blocks; we
// parse the primary HDU plus an HDU walk to count extensions.
//
// Detection is magic + extension: FITS files start with the literal
// ASCII bytes "SIMPLE  =" (one space + one space + equals at col 9),
// the most deterministic prefix in the format. Extensions cover the
// canonical `.fits` / `.fit` / `.fts` filename conventions.
type fitsType struct{}

func (f *fitsType) Name() string       { return "science/fits" }
func (f *fitsType) Extensions() []string { return []string{".fits", ".fit", ".fts"} }
func (f *fitsType) MagicBytes() [][]byte {
	return [][]byte{[]byte("SIMPLE  =")}
}

// Attributes dispatches to the FITS parser. Header-only — we never
// read pixel data. Truncated / corrupt files surface empty attrs
// rather than failing the walk, matching the wider "broken file
// doesn't fail the walk" pattern.
func (f *fitsType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readFITSInfo(fsys, path)
}

// scienceAttrs packs the cross-format surface (always present:
// science_format) plus per-format extras. Kept lean so future
// VOTable / HDF5 / PDS / CDF additions share the same shape under
// the `is_science_data` umbrella. Mirrors archiveAttrs / binaryAttrs
// / diskImageAttrs / installPackageAttrs / bytecodeAttrs.
func scienceAttrs(format string, extras Attributes) Attributes {
	out := Attributes{
		"science_format": format,
	}
	maps.Copy(out, extras)
	return out
}

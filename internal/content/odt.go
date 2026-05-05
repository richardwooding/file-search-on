package content

import (
	"archive/zip"
	"context"
	"io/fs"
)

func init() {
	Register(&odtType{})
}

type odtType struct{}

func (o *odtType) Name() string         { return "office/odt" }
func (o *odtType) Extensions() []string { return []string{".odt"} }
func (o *odtType) MagicBytes() [][]byte { return nil }

// Attributes reads OpenDocument metadata from `meta.xml`. ODT differs from
// OOXML only in the metadata entry path; the inner element vocabulary is the
// same Dublin Core triple, so readZipDublinCore handles the parsing.
func (o *odtType) Attributes(ctx context.Context, fsys fs.FS, filePath string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return nil, err
	}

	title, author, lang := readZipDublinCore(ctx, zr, "meta.xml")
	return officeAttributes(title, author, lang), nil
}

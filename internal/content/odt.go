package content

import (
	"archive/zip"
	"context"
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
func (o *odtType) Attributes(ctx context.Context, filePath string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	title, author, lang := readZipDublinCore(ctx, &zr.Reader, "meta.xml")
	return officeAttributes(title, author, lang), nil
}

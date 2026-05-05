package content

import (
	"archive/zip"
	"context"
	"io/fs"
)

// ooxmlAttributes is the shared body for DOCX/XLSX/PPTX. All three are
// OOXML zip packages that store metadata in `docProps/core.xml`.
func ooxmlAttributes(ctx context.Context, fsys fs.FS, filePath string) (Attributes, error) {
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

	title, author, lang := readZipDublinCore(ctx, zr, "docProps/core.xml")
	return officeAttributes(title, author, lang), nil
}

// officeAttributes packs the (possibly empty) Dublin Core triple into the
// shared attribute shape used by all four office content types. Title and
// author are omitted when empty so the activation defaults stand; language
// is always populated so callers can filter on `language == ""`.
func officeAttributes(title, author, lang string) Attributes {
	attrs := Attributes{
		"language": lang,
	}
	if title != "" {
		attrs["title"] = title
	}
	if author != "" {
		attrs["author"] = author
	}
	return attrs
}

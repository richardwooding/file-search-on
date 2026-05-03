package content

import (
	"archive/zip"
	"context"
)

// ooxmlAttributes is the shared body for DOCX/XLSX/PPTX. All three are
// OOXML zip packages that store metadata in `docProps/core.xml`.
func ooxmlAttributes(ctx context.Context, filePath string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	title, author, lang := readZipDublinCore(ctx, &zr.Reader, "docProps/core.xml")
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

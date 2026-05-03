package content

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io"
)

func init() {
	Register(&epubType{})
}

type epubType struct{}

func (e *epubType) Name() string         { return "epub" }
func (e *epubType) Extensions() []string { return []string{".epub"} }
func (e *epubType) MagicBytes() [][]byte { return nil }

func (e *epubType) Attributes(ctx context.Context, filePath string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	opfPath, err := readOPFPath(&zr.Reader)
	if err != nil || opfPath == "" {
		return Attributes{}, nil
	}

	title, author, lang := readZipDublinCore(ctx, &zr.Reader, opfPath)

	attrs := Attributes{
		"language": lang,
	}
	if title != "" {
		attrs["title"] = title
	}
	if author != "" {
		attrs["author"] = author
	}
	return attrs, nil
}

// readOPFPath parses META-INF/container.xml and returns the path to the OPF rootfile.
func readOPFPath(zr *zip.Reader) (string, error) {
	f, err := openZipEntry(zr, "META-INF/container.xml")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "rootfile" {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "full-path" {
				return attr.Value, nil
			}
		}
	}
	return "", nil
}

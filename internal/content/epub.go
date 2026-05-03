package content

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

func init() {
	Register(&epubType{})
}

type epubType struct{}

func (e *epubType) Name() string         { return "epub" }
func (e *epubType) Extensions() []string { return []string{".epub"} }
func (e *epubType) MagicBytes() [][]byte { return nil }

func (e *epubType) Attributes(filePath string) (Attributes, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	opfPath, err := readOPFPath(&zr.Reader)
	if err != nil || opfPath == "" {
		return Attributes{}, nil
	}

	title, author, lang := readOPFMetadata(&zr.Reader, opfPath)

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

// readOPFMetadata streams the OPF file and pulls out dc:title, dc:creator, dc:language.
func readOPFMetadata(zr *zip.Reader, opfPath string) (title, author, lang string) {
	f, err := openZipEntry(zr, opfPath)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			return
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch strings.ToLower(se.Name.Local) {
		case "title":
			if title == "" {
				title = strings.TrimSpace(decodeText(dec))
			}
		case "creator":
			if author == "" {
				author = strings.TrimSpace(decodeText(dec))
			}
		case "language":
			if lang == "" {
				lang = strings.TrimSpace(decodeText(dec))
			}
		}
	}
}

// decodeText reads the next chardata up to the matching end element, then returns it.
func decodeText(dec *xml.Decoder) string {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return sb.String()
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return sb.String()
		}
	}
}

// openZipEntry finds an entry by exact name (case-insensitive on the full path).
func openZipEntry(zr *zip.Reader, name string) (io.ReadCloser, error) {
	target := strings.ToLower(path.Clean(name))
	for _, zf := range zr.File {
		if strings.ToLower(path.Clean(zf.Name)) == target {
			return zf.Open()
		}
	}
	return nil, io.EOF
}

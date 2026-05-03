package content

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

// readDublinCore streams an XML reader looking for the standard Dublin Core
// metadata elements (`dc:title`, `dc:creator`, `dc:language`) and returns
// their first non-empty values. Used by EPUB OPF, OOXML `docProps/core.xml`,
// and ODT `meta.xml` — three formats that all embed dc:* elements with the
// same local names. Token-streaming so the whole document is never loaded.
func readDublinCore(r io.Reader) (title, author, language string) {
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err != nil {
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
			if language == "" {
				language = strings.TrimSpace(decodeText(dec))
			}
		}
	}
}

// decodeText reads char-data tokens until the matching EndElement, then returns
// the accumulated text.
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

// openZipEntry finds an entry by exact name (case-insensitive on the full
// path). Returns io.EOF when the entry does not exist so callers can treat
// missing-entry the same as end-of-stream.
func openZipEntry(zr *zip.Reader, name string) (io.ReadCloser, error) {
	target := strings.ToLower(path.Clean(name))
	for _, zf := range zr.File {
		if strings.ToLower(path.Clean(zf.Name)) == target {
			return zf.Open()
		}
	}
	return nil, io.EOF
}

// readZipDublinCore opens a single zip entry and runs the Dublin Core scanner
// against its contents. Returns zero values when the entry is missing or the
// scan finds nothing.
func readZipDublinCore(zr *zip.Reader, entry string) (title, author, language string) {
	rc, err := openZipEntry(zr, entry)
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	return readDublinCore(rc)
}

package content

import (
	"context"
	"encoding/xml"
	"io"
	"io/fs"
	"strings"

	"github.com/ledongthuc/pdf"
)

func init() {
	Register(&pdfType{})
}

type pdfType struct{}

func (p *pdfType) Name() string         { return "pdf" }
func (p *pdfType) Extensions() []string { return []string{".pdf"} }
func (p *pdfType) MagicBytes() [][]byte {
	return [][]byte{
		[]byte("%PDF"),
	}
}

// Attributes opens the PDF and pulls four pieces of metadata:
//
//   - title from the /Info dict
//   - author from the /Info dict
//   - page_count from the page tree
//   - language from /Root/Lang (catalog), falling back to /Root/Metadata
//     XMP <dc:language> when the catalog entry is empty
//
// Each individual lookup is sub-millisecond on real PDFs; ctx is checked
// at entry and between the catalog/XMP fallback paths so a cancelled walk
// stops promptly.
func (p *pdfType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ra, size, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	r, err := pdf.NewReader(ra, size)
	if err != nil {
		return nil, err
	}

	trailer := r.Trailer()
	root := trailer.Key("Root")
	info := trailer.Key("Info")

	attrs := Attributes{
		"page_count": int64(r.NumPage()),
	}
	if title := strings.TrimSpace(info.Key("Title").Text()); title != "" {
		attrs["title"] = title
	}
	if author := strings.TrimSpace(info.Key("Author").Text()); author != "" {
		attrs["author"] = author
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lang := strings.TrimSpace(root.Key("Lang").Text()); lang != "" {
		attrs["language"] = lang
	} else if lang := readPDFXMPLanguage(ctx, root.Key("Metadata")); lang != "" {
		attrs["language"] = lang
	}

	return attrs, nil
}

// readPDFXMPLanguage extracts a language code from a PDF XMP metadata stream
// by scanning for the first non-empty character data inside any `<dc:language>`
// element. Handles both the flat form and the RDF Bag form:
//
//	<dc:language>en</dc:language>
//	<dc:language><rdf:Bag><rdf:li>en</rdf:li></rdf:Bag></dc:language>
//
// Returns "" if metadata is not a stream or no language is found.
// ctx is checked at the top of every outer XML token loop so a
// pathological XMP payload (huge metadata stream, deeply nested
// language elements) surrenders to a cancelled context.
func readPDFXMPLanguage(ctx context.Context, metadata pdf.Value) string {
	if metadata.Kind() != pdf.Stream {
		return ""
	}
	rc := metadata.Reader()
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	dec.Strict = false
	for {
		if ctx.Err() != nil {
			return ""
		}
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok || strings.ToLower(se.Name.Local) != "language" {
			continue
		}
		// Walk the dc:language subtree gathering the first non-empty char data.
		depth := 1
		for depth > 0 {
			tok, err := dec.Token()
			if err != nil {
				return ""
			}
			switch t := tok.(type) {
			case xml.StartElement:
				depth++
			case xml.EndElement:
				depth--
			case xml.CharData:
				if v := strings.TrimSpace(string(t)); v != "" {
					return v
				}
			}
		}
	}
}

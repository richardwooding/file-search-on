package content

import (
	"context"
	"encoding/xml"
	"io/fs"
)

func init() {
	Register(&xmlType{})
}

type xmlType struct{}

func (x *xmlType) Name() string { return "xml" }
func (x *xmlType) Extensions() []string {
	return []string{".xml", ".xsl", ".xslt", ".xsd", ".rss", ".atom"}
}
func (x *xmlType) MagicBytes() [][]byte {
	return [][]byte{
		[]byte("<?xml"),
	}
}

func (x *xmlType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	decoder := xml.NewDecoder(f)
	var rootElement string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			rootElement = se.Name.Local
			break
		}
	}
	return Attributes{
		"root_element": rootElement,
	}, nil
}

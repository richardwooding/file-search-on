package content

import (
	"encoding/xml"
	"os"
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

func (x *xmlType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoder := xml.NewDecoder(f)
	var rootElement string
	for {
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

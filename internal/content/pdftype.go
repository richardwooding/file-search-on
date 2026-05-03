package content

import (
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func init() {
	Register(&pdfType{})
}

type pdfType struct{}

func (p *pdfType) Name() string { return "pdf" }
func (p *pdfType) Extensions() []string { return []string{".pdf"} }
func (p *pdfType) MagicBytes() [][]byte {
	return [][]byte{
		[]byte("%PDF"),
	}
}

func (p *pdfType) Attributes(path string) (Attributes, error) {
	pageCount, err := api.PageCountFile(path)
	if err != nil {
		pageCount = 0
	}
	return Attributes{
		"page_count": int64(pageCount),
		"author":     "",
	}, nil
}

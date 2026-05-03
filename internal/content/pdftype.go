package content

import (
	"context"

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

func (p *pdfType) Attributes(ctx context.Context, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// pdfcpu has no ctx variant; this call is uncancellable mid-flight.
	pageCount, err := api.PageCountFile(path)
	if err != nil {
		pageCount = 0
	}
	return Attributes{
		"page_count": int64(pageCount),
		"author":     "",
	}, nil
}

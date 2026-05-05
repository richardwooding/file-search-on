package content

import (
	"context"
	"io/fs"
)

func init() {
	Register(&pptxType{})
}

type pptxType struct{}

func (p *pptxType) Name() string         { return "office/pptx" }
func (p *pptxType) Extensions() []string { return []string{".pptx"} }
func (p *pptxType) MagicBytes() [][]byte { return nil }
func (p *pptxType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	return ooxmlAttributes(ctx, fsys, path)
}

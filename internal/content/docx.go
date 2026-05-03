package content

import "context"

func init() {
	Register(&docxType{})
}

type docxType struct{}

func (d *docxType) Name() string         { return "office/docx" }
func (d *docxType) Extensions() []string { return []string{".docx"} }
func (d *docxType) MagicBytes() [][]byte { return nil }
func (d *docxType) Attributes(ctx context.Context, path string) (Attributes, error) {
	return ooxmlAttributes(ctx, path)
}

package content

import "context"

func init() {
	Register(&xlsxType{})
}

type xlsxType struct{}

func (x *xlsxType) Name() string         { return "office/xlsx" }
func (x *xlsxType) Extensions() []string { return []string{".xlsx"} }
func (x *xlsxType) MagicBytes() [][]byte { return nil }
func (x *xlsxType) Attributes(ctx context.Context, path string) (Attributes, error) {
	return ooxmlAttributes(ctx, path)
}

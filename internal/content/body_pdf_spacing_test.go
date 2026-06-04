package content

import (
	"testing"

	"github.com/ledongthuc/pdf"
)

// TestPDFRowsToText_WordSpacing is the regression for issue #323. Many
// PDFs (justified LaTeX output) come back from GetTextByRow as separate
// word-ish runs with zeroed geometry; pdfRowsToText must join them with
// spaces (not concatenate) so multi-word phrase search works. When
// geometry IS present it spaces on a word-width gap instead.
func TestPDFRowsToText_WordSpacing(t *testing.T) {
	t.Run("zeroed geometry: join runs with spaces", func(t *testing.T) {
		rows := pdf.Rows{
			&pdf.Row{Position: 100, Content: pdf.TextHorizontal{
				{S: "Attention"}, {S: "Is"}, {S: "All"}, {S: "You"}, {S: "Need"},
			}},
			&pdf.Row{Position: 90, Content: pdf.TextHorizontal{
				{S: "neural"}, {S: "machine"}, {S: "translation"},
			}},
		}
		got := pdfRowsToText(rows)
		want := "Attention Is All You Need\nneural machine translation"
		if got != want {
			t.Errorf("got %q\nwant %q", got, want)
		}
	})

	t.Run("present geometry: gap-based spacing", func(t *testing.T) {
		rows := pdf.Rows{
			&pdf.Row{Position: 100, Content: pdf.TextHorizontal{
				// "foo" then "bar" contiguous (no gap) -> "foobar"; then a
				// wide gap before "baz" -> space.
				{S: "foo", X: 0, W: 30, FontSize: 10},
				{S: "bar", X: 30, W: 30, FontSize: 10},
				{S: "baz", X: 90, W: 30, FontSize: 10}, // gap 30 >> 0.2*10
			}},
		}
		got := pdfRowsToText(rows)
		want := "foobar baz"
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("does not double existing spaces", func(t *testing.T) {
		rows := pdf.Rows{
			&pdf.Row{Position: 100, Content: pdf.TextHorizontal{
				{S: "hello "}, {S: "world"},
			}},
		}
		if got := pdfRowsToText(rows); got != "hello world" {
			t.Errorf("got %q want %q", got, "hello world")
		}
	})
}

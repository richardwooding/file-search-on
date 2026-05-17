package content

import (
	"context"
	"io/fs"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// pdfCIDPattern matches "(cid:N)" markers emitted by ledongthuc/pdf
// when it can't resolve a CID/Type 0 glyph through the font's
// ToUnicode CMap. These are pure noise for body search — strip them
// in one pass at the end of extraction.
var pdfCIDPattern = regexp.MustCompile(`\(cid:\d+\)`)

// pdfBody extracts the plain-text body of a PDF document, one page at a
// time, in numeric page order. Uses github.com/ledongthuc/pdf (already
// a direct dep for metadata in pdftype.go) — no new dependency.
//
// Fonts are pre-cached across pages: ledongthuc/pdf's Page.GetPlainText
// re-derives the font/CharMap on every call when passed nil, so a 1000-
// page document does O(pages²) font work without the cache. We walk
// every page once to build the union font map, then iterate again to
// extract text against the shared cache. The same pattern is used by
// the library's own Reader.GetPlainText (which we don't call directly
// because it has no ctx-cancellation hook and gives no per-page error
// granularity).
//
// Per-page contract: a panic or per-page error contributes "" for that
// page but does NOT drop the document — the library's PDF parser is
// "incomplete" by its own documentation, so one bad page is expected
// and other pages can still be readable. Encrypted PDFs and image-only
// (scanned) PDFs surface as empty body — agents can detect that via
// `is_pdf && size(body) == 0`.
//
// ctx is checked at function entry, between the font pre-pass and the
// extraction pass, and at the top of every page iteration in both
// passes. Cancellation surfaces what's been collected so far with the
// ctx.Err().
func pdfBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()

	// pdf.NewReader can panic on malformed input despite returning
	// an error in the documented path — the library uses panic for
	// many internal parse failures. Wrap the entire extraction in
	// defer/recover so the contract "malformed file → empty body,
	// not a panic" holds for any adversarial PDF.
	var out strings.Builder
	func() {
		defer func() {
			_ = recover() // swallow — return whatever was collected before the panic
		}()
		r, err := pdf.NewReader(ra, size)
		if err != nil || r == nil {
			return // corrupt / encrypted-without-password → empty body
		}
		numPages := r.NumPage()
		if numPages <= 0 {
			return
		}

		// Pass 1 — build a single font cache shared across pages.
		// Mirrors the library's own Reader.GetPlainText pattern.
		fonts := make(map[string]*pdf.Font)
		for i := 1; i <= numPages; i++ {
			if err := ctx.Err(); err != nil {
				return
			}
			func() {
				defer func() { _ = recover() }()
				p := r.Page(i)
				if p.V.IsNull() {
					return
				}
				for _, name := range p.Fonts() {
					if _, ok := fonts[name]; !ok {
						f := p.Font(name)
						fonts[name] = &f
					}
				}
			}()
		}

		if err := ctx.Err(); err != nil {
			return
		}

		// Pass 2 — extract text from each page using the cached fonts.
		// Per-page failures contribute nothing and don't stop the walk.
		for i := 1; i <= numPages; i++ {
			if err := ctx.Err(); err != nil {
				return
			}
			if maxBytes > 0 && out.Len() >= maxBytes {
				break
			}
			text := func() string {
				defer func() { _ = recover() }()
				p := r.Page(i)
				if p.V.IsNull() {
					return ""
				}
				s, err := p.GetPlainText(fonts)
				if err != nil {
					return ""
				}
				return s
			}()
			if text == "" {
				continue
			}
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(text)
		}
	}()

	// Cap to maxBytes if the page-boundary stop overshot, then strip
	// (cid:N) markers. The regex pass is cheap on typical body sizes
	// (sub-millisecond on 1 MiB of text) and dramatically cleans up
	// CJK / complex-script PDFs whose ToUnicode CMap is incomplete.
	result := out.String()
	if maxBytes > 0 && len(result) > maxBytes {
		result = result[:maxBytes]
	}
	result = pdfCIDPattern.ReplaceAllString(result, "")
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

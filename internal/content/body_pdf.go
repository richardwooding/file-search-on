package content

import (
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// pdfCIDPattern matches "(cid:N)" markers emitted by ledongthuc/pdf
// when it can't resolve a CID/Type 0 glyph through the font's
// ToUnicode CMap. These are pure noise for body search — strip them
// in one pass at the end of extraction.
var pdfCIDPattern = regexp.MustCompile(`\(cid:\d+\)`)

// defaultPDFBodyCap bounds the pdftotext output read when the caller
// passes maxBytes <= 0 (1 MiB, matching the body-cache default).
const defaultPDFBodyCap = 1 << 20

// pdfSpaceGapRatio is the fraction of the font size above which a
// horizontal gap between two text runs is treated as a word break.
// Inter-word spaces render ~0.25em wide; intra-word kerning is near
// zero, so 0.2em cleanly separates words without splitting them.
const pdfSpaceGapRatio = 0.2

// pdfRowsToText flattens GetTextByRow output into plain text, inserting
// a space between consecutive runs whose horizontal gap implies a word
// boundary (the library's own GetPlainText drops these — issue #323).
// Rows are newline-separated, in the order GetTextByRow returns them
// (top-to-bottom); runs within a row are already sorted left-to-right.
func pdfRowsToText(rows pdf.Rows) string {
	var out strings.Builder
	for _, row := range rows {
		if row == nil {
			continue
		}
		var line strings.Builder
		prevEnd := 0.0
		lastWasSpace := true // suppress a leading space on each row
		for _, t := range row.Content {
			if !lastWasSpace && !strings.HasPrefix(t.S, " ") {
				// When the library reports usable geometry (font size +
				// width), space on a word-width gap. Many PDFs (e.g.
				// justified arXiv papers) come back with zeroed X/W/
				// FontSize — there each run is already a word-ish token,
				// so default to a single separating space rather than
				// concatenating everything into one blob.
				gapKnown := t.FontSize > 0 && t.W > 0
				if !gapKnown || t.X-prevEnd > pdfSpaceGapRatio*t.FontSize {
					line.WriteByte(' ')
				}
			}
			line.WriteString(t.S)
			prevEnd = t.X + t.W
			lastWasSpace = strings.HasSuffix(t.S, " ")
		}
		s := strings.TrimRight(line.String(), " \t")
		if s == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
	}
	return out.String()
}

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

	// Prefer poppler's pdftotext when it's on PATH — it reconstructs word
	// spacing properly, which the pure-Go path below can only approximate
	// from ledongthuc/pdf's (often zeroed) glyph geometry (issue #333).
	// Any failure falls through to the pure-Go extractor, so this is a
	// best-effort quality boost, never a hard dependency.
	if bin := pdftotextBin(); bin != "" {
		if s, ok := pdfBodyViaPdftotext(ctx, bin, ra, size, maxBytes); ok {
			return s, nil
		}
	}

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

		// Extract text per page via GetTextByRow (positional) rather
		// than GetPlainText: the latter concatenates text-show operators
		// with no spacing, so "Attention Is All You Need" comes out as
		// "AttentionIsAllYouNeed" and multi-word body.contains /
		// find-matches queries miss (issue #323). pdfRowsToText
		// reconstructs word breaks from the horizontal gaps between text
		// runs. GetTextByRow resolves fonts per page internally, so no
		// shared font cache is needed. Per-page failures contribute
		// nothing and don't stop the walk.
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
				rows, err := p.GetTextByRow()
				if err != nil {
					return ""
				}
				return pdfRowsToText(rows)
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

// pdftotextBin returns the path to poppler's pdftotext binary, or "" if
// it isn't on PATH. Resolved per call (LookPath is cheap relative to PDF
// parsing) so tests can inject a stub via PATH.
func pdftotextBin() string {
	p, _ := exec.LookPath("pdftotext")
	return p
}

// pdfBodyViaPdftotext extracts text with poppler's pdftotext. pdftotext
// needs a seekable file, so the PDF bytes are copied to a temp file
// (works for archive entries / in-memory FSes too, where no OS path
// exists). Output is read from a pipe bounded to maxBytes so a huge PDF
// can't blow up memory. Returns ("", false) on ANY failure — missing
// output, non-zero exit, copy error — so the caller falls back to the
// pure-Go extractor. ctx cancels the subprocess via CommandContext.
func pdfBodyViaPdftotext(ctx context.Context, bin string, ra io.ReaderAt, size int64, maxBytes int) (string, bool) {
	tmp, err := os.CreateTemp("", "fso-pdf-*.pdf")
	if err != nil {
		return "", false
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, io.NewSectionReader(ra, 0, size)); err != nil {
		_ = tmp.Close()
		return "", false
	}
	if err := tmp.Close(); err != nil {
		return "", false
	}

	// "-q" quiet, UTF-8 output, Unix EOLs; write text to stdout ("-").
	cmd := exec.CommandContext(ctx, bin, "-q", "-enc", "UTF-8", "-eol", "unix", tmp.Name(), "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false
	}
	if err := cmd.Start(); err != nil {
		return "", false
	}
	limit := int64(maxBytes)
	if limit <= 0 {
		limit = defaultPDFBodyCap
	}
	data, _ := io.ReadAll(io.LimitReader(stdout, limit))
	if err := cmd.Wait(); err != nil {
		return "", false
	}
	if len(data) == 0 {
		return "", false
	}
	return string(data), true
}

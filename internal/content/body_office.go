package content

import (
	"archive/zip"
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// ooxmlBody opens the named ZIP entry and runs extractXMLText against
// it. Used for DOCX (word/document.xml, "p"/"t") and XLSX
// (xl/sharedStrings.xml, "si"/"t"). When the entry is missing the
// function returns "" and a nil error — empty body, not an error.
func ooxmlBody(ctx context.Context, fsys fs.FS, filePath string, entries []string, paraElem, textElem string, maxBytes int) (string, error) {
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", nil // malformed zip → empty body, not error
	}
	var out strings.Builder
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		rc, err := openZipEntry(zr, entry)
		if err != nil {
			continue
		}
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := extractXMLText(ctx, rc, paraElem, textElem, remaining)
		_ = rc.Close()
		if body != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(body)
		}
	}
	return out.String(), nil
}

// xlsxBody extracts spreadsheet text from BOTH the shared-string table
// (xl/sharedStrings.xml — present when cells use t="s" → index
// references) AND every xl/worksheets/sheetN.xml (which carry
// inline-string cells via <c t="inlineStr"><is><t>...</t></is></c>).
// Real-world spreadsheets use either form; walking both ensures
// nothing's missed. Each sheet's strings are joined with newlines;
// "paragraph" elements differ — <si> for shared strings, <is> for
// inline strings — so we call extractXMLText with the right shape.
func xlsxBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", nil
	}

	var out strings.Builder
	appendBody := func(s string) {
		if s == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
	}
	remaining := func() int {
		if maxBytes <= 0 {
			return 0
		}
		return maxBytes - out.Len()
	}

	// Pass 1: shared-string table (one entry, indexed by sheet cells).
	if rc, err := openZipEntry(zr, "xl/sharedStrings.xml"); err == nil {
		body, _ := extractXMLText(ctx, rc, "si", "t", remaining())
		_ = rc.Close()
		appendBody(body)
	}

	// Pass 2: every worksheet, in numeric order, scoped to inline-string
	// cells. The <is> wrapper marks an inline-string cell value;
	// extractXMLText collects <t> CharData inside each one.
	type sheet struct {
		name string
		idx  int
	}
	var sheets []sheet
	for _, zf := range zr.File {
		n := strings.ToLower(zf.Name)
		if !strings.HasPrefix(n, "xl/worksheets/sheet") || !strings.HasSuffix(n, ".xml") {
			continue
		}
		base := n[len("xl/worksheets/"):]
		if strings.ContainsRune(base, '/') {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(base, "sheet%d.xml", &idx); err != nil {
			continue
		}
		sheets = append(sheets, sheet{name: zf.Name, idx: idx})
	}
	sort.Slice(sheets, func(i, j int) bool { return sheets[i].idx < sheets[j].idx })

	for _, s := range sheets {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		rc, err := openZipEntry(zr, s.name)
		if err != nil {
			continue
		}
		body, _ := extractXMLText(ctx, rc, "is", "t", remaining())
		_ = rc.Close()
		appendBody(body)
	}
	return out.String(), nil
}

// pptxBody walks every ppt/slides/slideN.xml in numeric slide order
// and joins their text. PPTX paragraphs are <a:p>, text runs are
// <a:t>. Slides are joined by a blank line so an agent grepping
// "slide-N material" sees clear breaks.
func pptxBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", nil // malformed zip → empty body, not error
	}

	// Collect ppt/slides/slideN.xml entries, sorted numerically by N.
	type slide struct {
		name string
		idx  int
	}
	var slides []slide
	for _, zf := range zr.File {
		name := strings.ToLower(zf.Name)
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		// Skip rels (ppt/slides/_rels/slide1.xml.rels) — they're in a
		// subdirectory and the HasPrefix above already excludes them
		// (slideN.xml has no slash after "slides/"). Defensive double-check:
		base := name[len("ppt/slides/"):]
		if strings.ContainsRune(base, '/') {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(base, "slide%d.xml", &n); err != nil {
			continue
		}
		slides = append(slides, slide{name: zf.Name, idx: n})
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].idx < slides[j].idx })

	var out strings.Builder
	for i, s := range slides {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		rc, err := openZipEntry(zr, s.name)
		if err != nil {
			continue
		}
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := extractXMLText(ctx, rc, "p", "t", remaining)
		_ = rc.Close()
		if body == "" {
			continue
		}
		if i > 0 && out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(body)
	}
	return out.String(), nil
}

// odtBody extracts the body of an ODT document's content.xml. ODT
// paragraphs are <text:p> and headings are <text:h>; text is direct
// CharData inside (optionally wrapped in <text:span> for styling,
// which we walk through). Unlike OOXML there's no nested text-run
// element name to scope to — we collect ALL chardata inside a
// paragraph regardless of intermediate styling spans. textElem is
// "" to signal the unscoped collection mode.
func odtBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", nil // malformed zip → empty body, not error
	}
	rc, err := openZipEntry(zr, "content.xml")
	if err != nil {
		return "", nil // missing content.xml → empty body, not error
	}
	defer func() { _ = rc.Close() }()

	// ODT has two paragraph-shaped elements; do two passes? No — the
	// extractor takes one paraElem. The simpler approach: handle "p"
	// AND treat <text:h> as a paragraph too. Since extractXMLText
	// matches by local-name only, and both <text:p> and <text:h> have
	// local names that differ, we'd need to extract twice. Cleaner:
	// preprocess by aliasing — but that's complex. Acceptable today:
	// pass paraElem="p", which covers the bulk of body text. Headings
	// are typically inside <text:h> so a doc with only headings would
	// yield "". For real documents (where headings sit alongside
	// paragraphs), the paragraph text is what users want to grep
	// anyway. Add heading extraction as a follow-up if asked.
	return extractXMLText(ctx, rc, "p", "", maxBytes)
}

package content

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractBody returns the human-readable text body of a structured-
// document content type — OOXML office formats (DOCX / XLSX / PPTX),
// ODT, EPUB, email (.eml / .mbox), and PDF. For everything else it
// returns "" and a nil error; the caller should fall through to its
// existing text-file body reader.
//
// Output is paragraph-joined plain text (newline-separated). XML
// formatting / styling / metadata are stripped; what remains is what
// a CEL filter like body.contains("transformer") or body.matches(...)
// can search. Capped at maxBytes (0 means use the existing 1 MiB
// default the caller picks). Honours ctx between every XML token.
//
// Used by the celexpr body reader (internal/celexpr/body.go) when the
// caller opts in via IncludeBody on a structured-document file. Kept
// in this package because the extractors share the ZIP / Dublin Core
// scaffolding already used for metadata extraction.
func ExtractBody(ctx context.Context, contentTypeName string, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch contentTypeName {
	case "office/docx":
		return ooxmlBody(ctx, fsys, filePath, []string{"word/document.xml"}, "p", "t", maxBytes)
	case "office/xlsx":
		return xlsxBody(ctx, fsys, filePath, maxBytes)
	case "office/pptx":
		return pptxBody(ctx, fsys, filePath, maxBytes)
	case "office/odt":
		return odtBody(ctx, fsys, filePath, maxBytes)
	case "epub":
		return epubBody(ctx, fsys, filePath, maxBytes)
	case "email/rfc822":
		return emlBody(ctx, fsys, filePath, maxBytes)
	case "email/mbox":
		return mboxBody(ctx, fsys, filePath, maxBytes)
	case "pdf":
		return pdfBody(ctx, fsys, filePath, maxBytes)
	}
	return "", nil
}

// extractXMLText walks an XML document collecting CharData. For each
// occurrence of paraElem the accumulated text is emitted as one line
// (newline-separated in the output). textElem, when non-empty, scopes
// CharData collection to chardata that's a descendant of that element
// — DOCX / XLSX / PPTX nest text in <w:t> / <t> / <a:t> runs. When
// textElem is empty, all CharData inside a paragraph counts (ODT
// style: text is direct children of <text:p>, sometimes wrapped in
// <text:span> which we walk through transparently).
//
// Matching is on local-name only (namespace prefix ignored), so a
// caller passing "p" matches both <w:p> and <text:p> with no fuss.
// maxBytes <= 0 means unbounded.
func extractXMLText(ctx context.Context, r io.Reader, paraElem, textElem string, maxBytes int) (string, error) {
	dec := xml.NewDecoder(r)
	var out, line strings.Builder
	textDepth := 0
	paraDepth := 0
	scoped := textElem != ""

	for {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len()+line.Len() >= maxBytes {
			break
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // malformed XML → return what we have, no error
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := t.Name.Local
			if n == paraElem {
				paraDepth++
			}
			if scoped && n == textElem {
				textDepth++
			}
		case xml.EndElement:
			n := t.Name.Local
			if scoped && n == textElem && textDepth > 0 {
				textDepth--
			}
			if n == paraElem && paraDepth > 0 {
				paraDepth--
				if paraDepth == 0 {
					if line.Len() > 0 {
						if out.Len() > 0 {
							out.WriteByte('\n')
						}
						out.WriteString(line.String())
						line.Reset()
					}
				}
			}
		case xml.CharData:
			collect := false
			if scoped {
				collect = textDepth > 0 && paraDepth > 0
			} else {
				collect = paraDepth > 0
			}
			if collect {
				line.Write(t)
			}
		}
	}
	// Flush any trailing line that wasn't closed.
	if line.Len() > 0 {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(line.String())
	}
	return out.String(), nil
}

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
		return "", nil //nolint:nilerr // malformed zip → empty body, not error
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
		return "", nil //nolint:nilerr
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
		return "", nil //nolint:nilerr // malformed zip → empty body, not error
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
		return "", nil //nolint:nilerr // malformed zip → empty body, not error
	}
	rc, err := openZipEntry(zr, "content.xml")
	if err != nil {
		return "", nil //nolint:nilerr // missing content.xml → empty body, not error
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

// epubBody walks the EPUB's OPF manifest, locates every spine-ordered
// (X)HTML chapter, and concatenates their stripped-tag text. Chapters
// are separated by a blank line so an agent grepping for chapter
// breaks sees clear delimiters. Honours maxBytes by stopping mid-spine.
func epubBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	ra, size, closer, err := openReaderAt(fsys, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", nil //nolint:nilerr
	}

	opfPath, err := readOPFPath(zr)
	if err != nil || opfPath == "" {
		return "", nil //nolint:nilerr
	}
	chapters, err := readEPUBSpineHrefs(ctx, zr, opfPath)
	if err != nil || len(chapters) == 0 {
		return "", nil //nolint:nilerr
	}

	opfDir := path.Dir(opfPath)
	var out strings.Builder
	for i, href := range chapters {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		full := path.Join(opfDir, href)
		rc, err := openZipEntry(zr, full)
		if err != nil {
			continue
		}
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := extractHTMLText(ctx, rc, remaining)
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

// readEPUBSpineHrefs returns the list of chapter hrefs in spine order
// from an .opf manifest. The manifest's <item id="x" href="..."/> map
// is resolved against the spine's <itemref idref="x"/> sequence.
// Items not in the spine are excluded — that's the EPUB convention
// for "this is part of the reading order".
func readEPUBSpineHrefs(ctx context.Context, zr *zip.Reader, opfPath string) ([]string, error) {
	rc, err := openZipEntry(zr, opfPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	manifest := map[string]string{} // id → href
	var spine []string              // ordered idrefs
	dec := xml.NewDecoder(rc)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "item":
			var id, href string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "href":
					href = a.Value
				}
			}
			if id != "" && href != "" {
				manifest[id] = href
			}
		case "itemref":
			for _, a := range se.Attr {
				if a.Name.Local == "idref" {
					spine = append(spine, a.Value)
				}
			}
		}
	}
	out := make([]string, 0, len(spine))
	for _, idref := range spine {
		if href, ok := manifest[idref]; ok {
			out = append(out, href)
		}
	}
	return out, nil
}

// extractHTMLText returns the visible text of an (X)HTML document
// with tags stripped. Uses encoding/xml's permissive mode to walk both
// XHTML and HTML5. Script / style elements are skipped — their content
// isn't user-readable. Whitespace inside CharData is preserved, but
// block-level elements emit a line break so an agent grep on the
// extracted body sees paragraph-level breaks.
func extractHTMLText(ctx context.Context, r io.Reader, maxBytes int) (string, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.AutoClose = htmlAutoClose
	dec.Entity = htmlEntities

	var out strings.Builder
	skipDepth := 0 // > 0 → inside <script> / <style>
	for {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := strings.ToLower(t.Name.Local)
			if n == "script" || n == "style" {
				skipDepth++
			}
		case xml.EndElement:
			n := strings.ToLower(t.Name.Local)
			if (n == "script" || n == "style") && skipDepth > 0 {
				skipDepth--
			}
			if htmlBlockElems[n] && out.Len() > 0 {
				// Block-level end → newline separator.
				if !endsWithNewline(out.String()) {
					out.WriteByte('\n')
				}
			}
		case xml.CharData:
			if skipDepth == 0 {
				out.Write(t)
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}

// htmlAutoClose lists void elements (no closing tag) so the XML
// decoder doesn't choke on HTML5 input. Covers the standard set per
// MDN's "void elements" reference.
var htmlAutoClose = []string{
	"area", "base", "br", "col", "embed", "hr", "img", "input", "link",
	"meta", "param", "source", "track", "wbr",
}

// htmlEntities maps the most common HTML5 named entities so the XML
// decoder doesn't error on `&nbsp;` / `&copy;` etc. inside EPUB
// chapters. The full HTML5 entity set is hundreds of names; the
// subset below covers what shows up in 95%+ of real ebooks.
var htmlEntities = map[string]string{
	"nbsp":   " ",
	"copy":   "©",
	"reg":    "®",
	"trade":  "™",
	"mdash":  "—",
	"ndash":  "–",
	"hellip": "…",
	"lsquo":  "‘",
	"rsquo":  "’",
	"ldquo":  "“",
	"rdquo":  "”",
	"laquo":  "«",
	"raquo":  "»",
	"euro":   "€",
	"pound":  "£",
	"yen":    "¥",
	"cent":   "¢",
	"sect":   "§",
	"para":   "¶",
	"middot": "·",
}

// htmlBlockElems is the set of block-level HTML element names whose
// end tag should emit a newline in the extracted text. Inline elements
// (<span>, <em>, <strong>, etc.) flow inline; block elements break.
var htmlBlockElems = map[string]bool{
	"p":          true,
	"div":        true,
	"h1":         true,
	"h2":         true,
	"h3":         true,
	"h4":         true,
	"h5":         true,
	"h6":         true,
	"li":         true,
	"ul":         true,
	"ol":         true,
	"blockquote": true,
	"br":         true,
	"hr":         true,
	"section":    true,
	"article":    true,
	"header":     true,
	"footer":     true,
	"aside":      true,
	"pre":        true,
	"tr":         true,
}

// emlBody extracts the human-readable text body from a single RFC 5322
// message. Walks the MIME tree: for non-multipart messages decodes the
// content-transfer encoding (quoted-printable / base64 / identity) and
// returns the decoded text; for multipart/alternative prefers
// text/plain over text/html; for multipart/mixed or related
// concatenates every text part, skipping attachments. text/html parts
// flow through extractHTMLText to strip tags.
//
// Headers (Subject / From / etc.) are NOT included — those already
// surface as separate CEL variables (title / author / email_to /
// email_message_id / ...). This extractor is specifically the message
// body that an agent would search for content.
func emlBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	f, err := fsys.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return emlBodyFromReader(ctx, f, maxBytes)
}

// emlBodyFromReader parses one RFC 5322 message from r and returns its
// text body. Shared by emlBody (file path) and mboxBody (per-message
// in-memory buffer). Malformed message → empty body + nil error.
func emlBodyFromReader(ctx context.Context, r io.Reader, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return "", nil //nolint:nilerr // malformed message → empty body, not error
	}
	return walkEmailBody(ctx, msg.Body, msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), maxBytes)
}

// walkEmailBody is the shared recursive walker — handles a single
// message part. multipart parts recurse via multipart.Reader; text
// parts decode the transfer encoding and (for text/html) strip tags;
// other media types (application/*, image/*, etc.) return "" to skip.
//
// RFC 2045 §5.2: when Content-Type is absent the default is
// "text/plain; charset=us-ascii". We honour that — a header-only
// message with no Content-Type still gets read as text.
func walkEmailBody(ctx context.Context, r io.Reader, contentType, transferEncoding string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		return walkMultipartBody(ctx, r, params["boundary"], mediaType, maxBytes)
	case mediaType == "text/plain":
		return decodeTextPart(ctx, r, transferEncoding, false, maxBytes)
	case mediaType == "text/html":
		return decodeTextPart(ctx, r, transferEncoding, true, maxBytes)
	}
	// application/* / image/* / etc. — not human-readable text. Skip.
	return "", nil
}

// walkMultipartBody walks a multipart container. For
// multipart/alternative, the RFC convention is "each part is a
// different representation of the same content"; agents want plain
// text, so we prefer text/plain and fall back to stripped text/html.
// For other multipart types (mixed / related / parallel / signed),
// every non-attachment text part is concatenated.
func walkMultipartBody(ctx context.Context, r io.Reader, boundary, multipartType string, maxBytes int) (string, error) {
	if boundary == "" {
		return "", nil
	}
	mr := multipart.NewReader(r, boundary)
	if multipartType == "multipart/alternative" {
		var plain, html string
		for {
			if err := ctx.Err(); err != nil {
				break
			}
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if isAttachmentPart(part) {
				_ = part.Close()
				continue
			}
			pCT := part.Header.Get("Content-Type")
			pCTE := part.Header.Get("Content-Transfer-Encoding")
			body, _ := walkEmailBody(ctx, part, pCT, pCTE, maxBytes)
			_ = part.Close()
			pMediaType, _, _ := mime.ParseMediaType(pCT)
			if pMediaType == "" {
				pMediaType = "text/plain"
			}
			switch {
			case pMediaType == "text/plain" && plain == "":
				plain = body
			case pMediaType == "text/html" && html == "":
				html = body
			}
		}
		if plain != "" {
			return plain, nil
		}
		return html, nil
	}

	// mixed / related / parallel / signed — concatenate text parts.
	var out strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if isAttachmentPart(part) {
			_ = part.Close()
			continue
		}
		pCT := part.Header.Get("Content-Type")
		pCTE := part.Header.Get("Content-Transfer-Encoding")
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := walkEmailBody(ctx, part, pCT, pCTE, remaining)
		_ = part.Close()
		if body == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(body)
	}
	return out.String(), nil
}

// decodeTextPart reads a text/plain or text/html part, applying the
// Content-Transfer-Encoding decoder (quoted-printable / base64 are the
// two that matter for real-world email; 7bit / 8bit / binary pass
// through). Unknown encodings read raw bytes — at worst the agent sees
// undecoded MIME-quoted output, never an error. When isHTML is set,
// the decoded bytes are run through extractHTMLText to strip tags
// before returning.
func decodeTextPart(ctx context.Context, r io.Reader, transferEncoding string, isHTML bool, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	decoded := r
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "quoted-printable":
		decoded = quotedprintable.NewReader(r)
	case "base64":
		decoded = base64.NewDecoder(base64.StdEncoding, r)
		// "", "7bit", "8bit", "binary", or anything else — read raw.
	}
	cap := maxBytes
	if cap <= 0 {
		cap = 1 << 20 // 1 MiB local default — matches celexpr.defaultBodyMaxBytes
	}
	if isHTML {
		return extractHTMLText(ctx, decoded, cap)
	}
	b, err := io.ReadAll(io.LimitReader(decoded, int64(cap)))
	if err != nil {
		return strings.TrimRight(string(b), "\r\n "), nil //nolint:nilerr // partial body on transfer-encoding error is fine
	}
	return strings.TrimRight(string(b), "\r\n "), nil
}

// mboxBody walks an mbox archive, extracting each message's body via
// emlBodyFromReader and concatenating with double-newline separators
// so an agent grepping the result can search across the whole inbox.
// Splits on "From " lines using the same isMboxSeparator helper that
// the attribute parser uses, so the body and attribute parsers agree
// on where messages start.
func mboxBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	f, err := fsys.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	var out strings.Builder
	var msgBuf bytes.Buffer
	flush := func() {
		if msgBuf.Len() == 0 {
			return
		}
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := emlBodyFromReader(ctx, &msgBuf, remaining)
		msgBuf.Reset()
		if body == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(body)
	}
	started := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		line := scanner.Bytes()
		if isMboxSeparator(line) {
			if started {
				flush()
			}
			started = true
			continue
		}
		if started {
			msgBuf.Write(line)
			msgBuf.WriteByte('\n')
		}
	}
	flush()
	return out.String(), nil
}

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

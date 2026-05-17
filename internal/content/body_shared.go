package content

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
)

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
	"nbsp":   " ",
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

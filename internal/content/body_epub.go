package content

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io/fs"
	"path"
	"strings"
)

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
		return "", nil
	}

	opfPath, err := readOPFPath(ctx, zr)
	if err != nil || opfPath == "" {
		return "", nil
	}
	chapters, err := readEPUBSpineHrefs(ctx, zr, opfPath)
	if err != nil || len(chapters) == 0 {
		return "", nil
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

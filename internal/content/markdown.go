package content

import (
	"bytes"
	"context"
	"io/fs"
)

func init() {
	Register(&markdownType{})
}

type markdownType struct{}

func (m *markdownType) Name() string { return "markdown" }
func (m *markdownType) Extensions() []string {
	return []string{".md", ".markdown", ".mdown", ".mkd"}
}
func (m *markdownType) MagicBytes() [][]byte { return nil }

func (m *markdownType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readAll(fsys, path)
	if err != nil {
		return nil, err
	}

	fm, body := splitFrontmatter(data)

	// Body is already in memory, so we don't need a bufio.Scanner —
	// just walk newlines directly with bytes.Cut. Avoids the 64 KiB
	// upfront scanner-buffer allocation that dominated this path.
	// Title detection uses bytes.HasPrefix to avoid the per-line
	// []byte→string copy; bytes.Fields skips it for word counting
	// too. The ATX-heading rule (Markdown spec): "# " followed by
	// non-empty text on the same line — we keep the same heuristic.
	var title string
	var wordCount int
	remaining := body
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var line []byte
		line, remaining, _ = bytes.Cut(remaining, []byte("\n"))
		line = bytes.TrimRight(line, "\r")
		if title == "" && bytes.HasPrefix(line, []byte("# ")) {
			title = string(line[2:])
		}
		wordCount += len(bytes.Fields(line))
	}

	attrs := Attributes{
		"word_count":         int64(wordCount),
		"frontmatter_format": "",
		"frontmatter":        map[string]any{},
		"tags":               []string{},
		"categories":         []string{},
		"draft":              false,
	}

	if fm != nil {
		attrs["frontmatter_format"] = fm.Format
		attrs["frontmatter"] = fm.Data
		if v, ok := stringValue(fm.Data["title"]); ok && v != "" {
			title = v
		}
		if v, ok := stringValue(fm.Data["author"]); ok {
			attrs["author"] = v
		}
		if v, ok := stringValue(fm.Data["language"]); ok {
			attrs["language"] = v
		}
		if tags, ok := stringListValue(fm.Data["tags"]); ok {
			attrs["tags"] = tags
		}
		if cats, ok := stringListValue(fm.Data["categories"]); ok {
			attrs["categories"] = cats
		}
		if v, ok := fm.Data["draft"].(bool); ok {
			attrs["draft"] = v
		}
		if t, ok := timeValue(fm.Data["date"]); ok {
			attrs["date"] = t
		}
	}

	attrs["title"] = title
	return attrs, nil
}

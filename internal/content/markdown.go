package content

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"strings"
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

	var title string
	var wordCount int
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		if title == "" && strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
		}
		wordCount += len(strings.Fields(line))
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

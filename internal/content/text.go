package content

import (
	"bufio"
	"context"
	"io/fs"
	"strings"
)

func init() {
	Register(&textType{})
}

type textType struct{}

func (t *textType) Name() string         { return "text" }
func (t *textType) Extensions() []string { return []string{".txt", ".text", ".log"} }
func (t *textType) MagicBytes() [][]byte { return nil }

func (t *textType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	var lineCount, wordCount int64
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineCount++
		wordCount += int64(len(strings.Fields(scanner.Text())))
	}

	return Attributes{
		"line_count": lineCount,
		"word_count": wordCount,
	}, nil
}

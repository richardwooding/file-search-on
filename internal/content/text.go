package content

import (
	"bufio"
	"os"
	"strings"
)

func init() {
	Register(&textType{})
}

type textType struct{}

func (t *textType) Name() string         { return "text" }
func (t *textType) Extensions() []string { return []string{".txt", ".text", ".log"} }
func (t *textType) MagicBytes() [][]byte { return nil }

func (t *textType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var lineCount, wordCount int64
	for scanner.Scan() {
		lineCount++
		wordCount += int64(len(strings.Fields(scanner.Text())))
	}

	return Attributes{
		"line_count": lineCount,
		"word_count": wordCount,
	}, nil
}

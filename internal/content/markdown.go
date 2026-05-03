package content

import (
	"bufio"
	"os"
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

func (m *markdownType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var title string
	var wordCount int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if title == "" && strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
		}
		wordCount += len(strings.Fields(line))
	}
	return Attributes{
		"title":      title,
		"word_count": int64(wordCount),
	}, nil
}

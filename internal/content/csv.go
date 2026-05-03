package content

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register(&csvType{})
}

type csvType struct{}

func (c *csvType) Name() string         { return "csv" }
func (c *csvType) Extensions() []string { return []string{".csv", ".tsv"} }
func (c *csvType) MagicBytes() [][]byte { return nil }

func (c *csvType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	delim := ","
	if strings.EqualFold(filepath.Ext(path), ".tsv") {
		delim = "\t"
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var columnCount int64
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		columnCount = int64(strings.Count(line, delim) + 1)
		break
	}

	return Attributes{
		"column_count": columnCount,
	}, nil
}

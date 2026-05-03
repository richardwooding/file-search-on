package content

import (
	"bufio"
	"encoding/csv"
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

	delim := ','
	if strings.EqualFold(filepath.Ext(path), ".tsv") {
		delim = '\t'
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	var firstLine string
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			firstLine = line
			break
		}
	}

	attrs := Attributes{
		"column_count": int64(0),
		"csv_columns":  []string{},
	}
	if firstLine == "" {
		return attrs, nil
	}

	r := csv.NewReader(strings.NewReader(firstLine))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	record, err := r.Read()
	if err != nil {
		// Malformed beyond LazyQuotes' tolerance — fall back to a raw split so
		// column_count still has a non-zero value rather than dropping the file.
		fields := strings.Split(firstLine, string(delim))
		attrs["column_count"] = int64(len(fields))
		attrs["csv_columns"] = fields
		return attrs, nil
	}

	attrs["column_count"] = int64(len(record))
	attrs["csv_columns"] = record
	return attrs, nil
}

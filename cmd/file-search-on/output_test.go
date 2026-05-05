package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// fixtureResults builds a fixed slice driving the printer tests.
func fixtureResults() []search.Result {
	return []search.Result{
		{
			Path:        "docs/intro.md",
			ContentType: "markdown",
			Size:        4321,
			Attrs: &celexpr.FileAttributes{
				ContentType: "markdown",
				Size:        4321,
				IsMarkdown:  true,
				Extra: content.Attributes{
					"title":              "Introduction",
					"author":             "Jane Doe",
					"language":           "en",
					"word_count":         int64(1543),
					"frontmatter_format": "yaml",
					"frontmatter":        map[string]any{"category": "guide", "draft": false},
				},
			},
		},
		{
			Path:        "data/sample.csv",
			ContentType: "csv",
			Size:        212,
			Attrs: &celexpr.FileAttributes{
				ContentType: "csv",
				Size:        212,
				IsCSV:       true,
				Extra: content.Attributes{
					"column_count": int64(4),
					"csv_columns":  []string{"a", "b", "c", "d"},
				},
			},
		},
	}
}

func TestPrintBare(t *testing.T) {
	var buf bytes.Buffer
	printBare(&buf, fixtureResults())
	got := buf.String()
	want := "docs/intro.md\ndata/sample.csv\n"
	if got != want {
		t.Errorf("printBare:\n got %q\nwant %q", got, want)
	}
}

func TestPrintDefault(t *testing.T) {
	var buf bytes.Buffer
	printDefault(&buf, fixtureResults())
	got := buf.String()
	if !strings.Contains(got, "docs/intro.md\t[markdown]\t4321 bytes") {
		t.Errorf("printDefault missing markdown row: %q", got)
	}
	if !strings.Contains(got, "data/sample.csv\t[csv]\t212 bytes") {
		t.Errorf("printDefault missing csv row: %q", got)
	}
}

func TestPrintVerbose(t *testing.T) {
	var buf bytes.Buffer
	printVerbose(&buf, fixtureResults())
	got := buf.String()
	for _, want := range []string{
		"docs/intro.md\n",
		"content_type   markdown",
		"size           4,321 bytes",
		"title         Introduction",
		"language      en",
		"word_count    1,543",
		"frontmatter   yaml (2 keys)",
		"data/sample.csv\n",
		"column_count  4",
		"csv_columns   a, b, c, d",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("printVerbose missing %q in:\n%s", want, got)
		}
	}
}

func TestPrintJSONLines(t *testing.T) {
	var buf bytes.Buffer
	if err := printJSON(&buf, fixtureResults()); err != nil {
		t.Fatalf("printJSON: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d:\n%s", len(lines), buf.String())
	}
	var first Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode line 0: %v\n%s", err, lines[0])
	}
	if first.Path != "docs/intro.md" || first.Title != "Introduction" || first.WordCount != 1543 {
		t.Errorf("decoded record 0: %+v", first)
	}
	// `omitempty` should drop is_csv on the markdown row.
	if first.IsCSV {
		t.Errorf("expected IsCSV false on markdown row")
	}
	// CSV row should have csv_columns populated.
	var second Record
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("decode line 1: %v\n%s", err, lines[1])
	}
	if !second.IsCSV || len(second.CSVColumns) != 4 {
		t.Errorf("decoded record 1: %+v", second)
	}
}

func TestPrintTemplate(t *testing.T) {
	tmpl, err := parseFormatTemplate(`{{.Path}}\t{{.Title}}\t{{.WordCount}}`)
	if err != nil {
		t.Fatalf("parseFormatTemplate: %v", err)
	}
	var buf bytes.Buffer
	if err := printTemplate(&buf, fixtureResults(), tmpl); err != nil {
		t.Fatalf("printTemplate: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "docs/intro.md\tIntroduction\t1543\n") {
		t.Errorf("template did not produce expected markdown line: %q", got)
	}
	// CSV row has no Title — empty string projected.
	if !strings.Contains(got, "data/sample.csv\t\t0\n") {
		t.Errorf("template did not produce expected csv line (with empty Title and WordCount=0): %q", got)
	}
}

// fixtureChan funnels fixtureResults() into a closed channel for the
// streaming-printer tests. Returns the channel ready to be ranged over.
func fixtureChan() <-chan search.Result {
	results := fixtureResults()
	ch := make(chan search.Result, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch
}

func TestPrintBareStream(t *testing.T) {
	var buf bytes.Buffer
	printBareStream(&buf, fixtureChan())
	got := buf.String()
	want := "docs/intro.md\ndata/sample.csv\n"
	if got != want {
		t.Errorf("printBareStream:\n got %q\nwant %q", got, want)
	}
}

func TestPrintJSONStream(t *testing.T) {
	var buf bytes.Buffer
	if err := printJSONStream(&buf, fixtureChan()); err != nil {
		t.Fatalf("printJSONStream: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d:\n%s", len(lines), buf.String())
	}
	var first Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode line 0: %v\n%s", err, lines[0])
	}
	if first.Path != "docs/intro.md" || first.Title != "Introduction" {
		t.Errorf("decoded streamed record 0: %+v", first)
	}
}

func TestPrintTemplateStream(t *testing.T) {
	tmpl, err := parseFormatTemplate(`{{.Path}}\t{{.Title}}`)
	if err != nil {
		t.Fatalf("parseFormatTemplate: %v", err)
	}
	var buf bytes.Buffer
	if err := printTemplateStream(&buf, fixtureChan(), tmpl); err != nil {
		t.Fatalf("printTemplateStream: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "docs/intro.md\tIntroduction\n") {
		t.Errorf("template stream did not produce expected line: %q", got)
	}
}

func TestRecordFromHandlesDate(t *testing.T) {
	r := search.Result{
		Path:        "post.md",
		ContentType: "markdown",
		Attrs: &celexpr.FileAttributes{
			ContentType: "markdown",
			Extra: content.Attributes{
				"date": time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	rec := recordFrom(r)
	if rec.Date != "2026-05-03T12:00:00Z" {
		t.Errorf("Date = %q, want RFC3339", rec.Date)
	}
}

func TestCommafy(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1500, "-1,500"},
	}
	for _, tc := range cases {
		if got := commafy(tc.in); got != tc.want {
			t.Errorf("commafy(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

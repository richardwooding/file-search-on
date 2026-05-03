package celexpr_test

import (
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

func TestEvaluate(t *testing.T) {
	eval, err := celexpr.New("size > 100 && is_json")
	if err != nil {
		t.Fatal(err)
	}

	attrs := &celexpr.FileAttributes{
		Name:        "test.json",
		Path:        "/home/runner/work/file-search-on/file-search-on/test.json",
		Dir:         "/home/runner/work/file-search-on/file-search-on",
		Size:        200,
		Ext:         ".json",
		ContentType: "json",
		IsJSON:      true,
	}

	match, err := eval.Evaluate(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected match, got no match")
	}
}

func TestEvaluateFalse(t *testing.T) {
	eval, err := celexpr.New("size > 100 && is_json")
	if err != nil {
		t.Fatal(err)
	}

	attrs := &celexpr.FileAttributes{
		Name:        "test.txt",
		Path:        "/home/runner/work/file-search-on/file-search-on/test.txt",
		Dir:         "/home/runner/work/file-search-on/file-search-on",
		Size:        50,
		Ext:         ".txt",
		ContentType: "",
		IsJSON:      false,
	}

	match, err := eval.Evaluate(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if match {
		t.Error("expected no match, got match")
	}
}

func TestEvaluateFrontmatter(t *testing.T) {
	date := time.Date(2024, 3, 14, 0, 0, 0, 0, time.UTC)
	extra := content.Attributes{
		"frontmatter_format": "yaml",
		"frontmatter": map[string]any{
			"title":    "Hello",
			"tags":     []any{"go", "cel"},
			"draft":    false,
			"category": "essay",
		},
		"tags":       []string{"go", "cel"},
		"categories": []string{},
		"draft":      false,
		"date":       date,
		"title":      "Hello",
		"author":     "Jane",
	}

	cases := []struct {
		expr string
		want bool
	}{
		{`is_markdown && draft == false && "go" in tags`, true},
		{`is_markdown && "rust" in tags`, false},
		{`is_markdown && frontmatter.category == "essay"`, true},
		{`is_markdown && frontmatter_format == "yaml"`, true},
		{`is_markdown && date > timestamp("2024-01-01T00:00:00Z")`, true},
		{`is_markdown && date < timestamp("2024-01-01T00:00:00Z")`, false},
		{`is_markdown && draft`, false},
	}

	attrs := &celexpr.FileAttributes{
		Name:        "post.md",
		Path:        "/repo/post.md",
		Dir:         "/repo",
		Size:        1234,
		Ext:         ".md",
		ContentType: "markdown",
		IsMarkdown:  true,
		Extra:       extra,
	}

	for _, tc := range cases {
		eval, err := celexpr.New(tc.expr)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.expr, err)
		}
		got, err := eval.Evaluate(attrs)
		if err != nil {
			t.Fatalf("eval %q: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("expr %q: got %v, want %v", tc.expr, got, tc.want)
		}
	}
}

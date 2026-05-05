package content_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func markdownAttrs(t *testing.T, path string) content.Attributes {
	t.Helper()
	for _, ct := range content.DefaultRegistry().Types() {
		if ct.Name() == "markdown" {
			attrs, err := attributesAt(t.Context(), ct, path)
			if err != nil {
				t.Fatal(err)
			}
			return attrs
		}
	}
	t.Fatal("markdown content type not registered")
	return nil
}

func TestYAMLFrontmatter(t *testing.T) {
	path := writeTemp(t, "yaml.md", `---
title: Hello World
author: Jane Doe
draft: false
tags:
  - go
  - search
categories: [docs]
date: 2024-03-14
custom_field: alpha
---

# Body heading

Body words one two three.
`)
	attrs := markdownAttrs(t, path)

	if got := attrs["frontmatter_format"]; got != "yaml" {
		t.Errorf("frontmatter_format = %v, want yaml", got)
	}
	if got := attrs["title"]; got != "Hello World" {
		t.Errorf("title = %v, want Hello World (front-matter overrides H1)", got)
	}
	if got := attrs["author"]; got != "Jane Doe" {
		t.Errorf("author = %v, want Jane Doe", got)
	}
	if got := attrs["draft"]; got != false {
		t.Errorf("draft = %v, want false", got)
	}
	tags, _ := attrs["tags"].([]string)
	if len(tags) != 2 || tags[0] != "go" || tags[1] != "search" {
		t.Errorf("tags = %v, want [go search]", tags)
	}
	cats, _ := attrs["categories"].([]string)
	if len(cats) != 1 || cats[0] != "docs" {
		t.Errorf("categories = %v, want [docs]", cats)
	}
	date, _ := attrs["date"].(time.Time)
	if date.Year() != 2024 || date.Month() != time.March || date.Day() != 14 {
		t.Errorf("date = %v, want 2024-03-14", date)
	}
	fm, _ := attrs["frontmatter"].(map[string]any)
	if fm["custom_field"] != "alpha" {
		t.Errorf("frontmatter.custom_field = %v, want alpha", fm["custom_field"])
	}
	// word_count should exclude the front-matter block.
	if got := attrs["word_count"].(int64); got > 10 {
		t.Errorf("word_count = %d, expected to exclude front-matter (~6 body words)", got)
	}
}

func TestTOMLFrontmatter(t *testing.T) {
	path := writeTemp(t, "toml.md", `+++
title = "TOML Doc"
draft = true
tags = ["rust", "cli"]
date = 2024-05-01
+++

Body.
`)
	attrs := markdownAttrs(t, path)
	if got := attrs["frontmatter_format"]; got != "toml" {
		t.Errorf("frontmatter_format = %v, want toml", got)
	}
	if got := attrs["title"]; got != "TOML Doc" {
		t.Errorf("title = %v, want TOML Doc", got)
	}
	if got := attrs["draft"]; got != true {
		t.Errorf("draft = %v, want true", got)
	}
	tags, _ := attrs["tags"].([]string)
	if len(tags) != 2 || tags[0] != "rust" || tags[1] != "cli" {
		t.Errorf("tags = %v, want [rust cli]", tags)
	}
}

func TestJSONFrontmatter(t *testing.T) {
	path := writeTemp(t, "json.md", `{
  "title": "JSON Doc",
  "tags": ["json", "fm"],
  "draft": false
}

# Body
`)
	attrs := markdownAttrs(t, path)
	if got := attrs["frontmatter_format"]; got != "json" {
		t.Errorf("frontmatter_format = %v, want json", got)
	}
	if got := attrs["title"]; got != "JSON Doc" {
		t.Errorf("title = %v, want JSON Doc", got)
	}
	tags, _ := attrs["tags"].([]string)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2 entries", tags)
	}
}

func TestNoFrontmatter(t *testing.T) {
	path := writeTemp(t, "plain.md", `# Plain heading

Just a body, no front-matter.
`)
	attrs := markdownAttrs(t, path)
	if got := attrs["frontmatter_format"]; got != "" {
		t.Errorf("frontmatter_format = %q, want empty", got)
	}
	if got := attrs["title"]; got != "Plain heading" {
		t.Errorf("title = %v, want Plain heading (H1 fallback)", got)
	}
}

func TestMalformedYAMLDegradesGracefully(t *testing.T) {
	path := writeTemp(t, "bad.md", `---
title: "unterminated
tags: [oops
---

# Fallback heading
`)
	attrs := markdownAttrs(t, path)
	// Malformed front-matter should be ignored; title falls back to H1.
	if got := attrs["frontmatter_format"]; got != "" {
		t.Errorf("frontmatter_format = %q, want empty for malformed input", got)
	}
	if got := attrs["title"]; got != "Fallback heading" {
		t.Errorf("title = %v, want Fallback heading", got)
	}
}

func TestFrontmatterLanguagePromoted(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"yaml.md", "---\ntitle: x\nlanguage: fr\n---\nbody\n"},
		{"toml.md", "+++\ntitle = \"x\"\nlanguage = \"fr\"\n+++\nbody\n"},
		{"json.md", "{\"title\": \"x\", \"language\": \"fr\"}\n\nbody\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, tc.name, tc.body)
			attrs := markdownAttrs(t, path)
			if got := attrs["language"]; got != "fr" {
				t.Errorf("language = %v, want fr", got)
			}
		})
	}
}

func TestFrontmatterLanguageAbsent(t *testing.T) {
	path := writeTemp(t, "no-lang.md", "---\ntitle: x\n---\nbody\n")
	attrs := markdownAttrs(t, path)
	if v, ok := attrs["language"]; ok {
		t.Errorf("language present when absent in front-matter: %v", v)
	}
}

func TestSingleStringTagWrapsAsList(t *testing.T) {
	path := writeTemp(t, "single.md", `---
tags: solo
---
body
`)
	attrs := markdownAttrs(t, path)
	tags, _ := attrs["tags"].([]string)
	if len(tags) != 1 || tags[0] != "solo" {
		t.Errorf("tags = %v, want [solo]", tags)
	}
}

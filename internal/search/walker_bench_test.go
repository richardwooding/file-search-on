package search_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// seedBenchTree writes a fixed, deterministic tree of markdown files
// to b.TempDir() and returns the root path. The shape is tuned to be
// large enough to amortise walker setup (~200 files) yet small enough
// that a single iteration finishes in tens of ms on typical hardware,
// so `go test -bench=. -benchtime=1s` collects a few hundred samples.
func seedBenchTree(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	for i := range 200 {
		// Varied content sizes so the body-read / line-count paths
		// see realistic input. Frontmatter on a third of the files so
		// markdown parsing exercises the YAML path too.
		body := fmt.Sprintf("# Title %d\n\nParagraph one.\n\nParagraph two with %d words of filler %s\n",
			i, i*10, repeat("lorem ipsum dolor sit amet ", i%20+1))
		if i%3 == 0 {
			body = "---\ntitle: Doc " + fmt.Sprint(i) + "\ndraft: true\ntags: [a, b, c]\n---\n" + body
		}
		path := filepath.Join(root, fmt.Sprintf("doc-%03d.md", i))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// BenchmarkWalk_Bare is the cheap path: match every file, return
// just (path, content_type, size) — no full FileAttributes pointer
// is retained. Measures pure walker + content-type-detection
// overhead.
func BenchmarkWalk_Bare(b *testing.B) {
	root := seedBenchTree(b)
	reg := content.DefaultRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := search.Walk(context.Background(), search.Options{
			Root: root,
			Expr: "true",
		}, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWalk_Predicate uses a type predicate but skips the
// attribute payload. This is the typical "find me all markdown
// files" shape — minimal per-file work beyond detection.
func BenchmarkWalk_Predicate(b *testing.B) {
	root := seedBenchTree(b)
	reg := content.DefaultRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := search.Walk(context.Background(), search.Options{
			Root: root,
			Expr: "is_markdown",
		}, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWalk_WithAttrs forces the full parse path: markdown
// frontmatter + word count for every match. This is what the MCP
// search tool does (it always sets IncludeAttributes=true) and what
// `-o verbose|json` uses on the CLI side.
func BenchmarkWalk_WithAttrs(b *testing.B) {
	root := seedBenchTree(b)
	reg := content.DefaultRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := search.Walk(context.Background(), search.Options{
			Root:              root,
			Expr:              "is_markdown && word_count > 10",
			IncludeAttributes: true,
		}, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkComputeStats measures the histogram aggregation path —
// same per-file work as Walk_WithAttrs (ComputeStats forces
// IncludeAttributes internally) plus the bucket-tallying loop.
func BenchmarkComputeStats(b *testing.B) {
	root := seedBenchTree(b)
	reg := content.DefaultRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := search.ComputeStats(context.Background(), search.Options{
			Root:    root,
			Expr:    "true",
			GroupBy: "content_type",
		}, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFindDuplicates exercises the two-pass dedup path: size
// bucketing (every file) + sha256 hashing of size-collision groups.
// On the seeded tree most files have unique sizes so only a handful
// reach the hash pass — this is the common "what's in this folder"
// case, not the pathological all-files-identical case.
func BenchmarkFindDuplicates(b *testing.B) {
	root := seedBenchTree(b)
	reg := content.DefaultRegistry()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := search.FindDuplicates(context.Background(), search.Options{
			Root: root,
			Expr: "true",
		}, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

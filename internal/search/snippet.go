package search

import (
	"bufio"
	"context"
	"io/fs"
	"strings"
)

// defaultSnippetLines is what we use when IncludeSnippet is true but
// SnippetLines is unset (zero). Ten lines is enough to convey what
// the file is about without flooding the response payload — a typical
// markdown post's frontmatter + a couple of paragraphs.
const defaultSnippetLines = 10

// snippetLineCap bounds the per-line buffer the snippet scanner will
// tolerate. Documents with absurdly long single lines (minified JSON,
// rolled-up logs) won't blow up the response; the offending line is
// truncated and we keep going. Independent of Options.MaxLineBytes,
// which gates content-type Attributes() parsing.
const snippetLineCap = 64 * 1024

// readSnippet returns the first n lines of the file at fsPath, joined
// by "\n". Returns an empty string + nil for non-text content types
// (caller responsibility — see isTextContentType). Errors during
// reading return ("", err); the caller decides whether to surface or
// swallow — the walker swallows so a broken file doesn't drop a match
// that already passed the CEL filter.
//
// ctx is checked at entry and threaded through the file open / read
// path so cancellation propagates promptly.
func readSnippet(ctx context.Context, fsys fs.FS, fsPath string, n int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if n <= 0 {
		n = defaultSnippetLines
	}

	f, err := fsys.Open(fsPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), snippetLineCap)

	var b strings.Builder
	count := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if count > 0 {
			b.WriteByte('\n')
		}
		b.Write(scanner.Bytes())
		count++
		if count >= n {
			break
		}
	}
	// Don't report scanner.Err() — pathological files (truncated UTF-8,
	// over-long lines) shouldn't drop the snippet entirely. Whatever
	// we managed to scan is good enough.
	return b.String(), nil
}

// isTextContentType reports whether a registered ContentType.Name() is
// text-based enough to make sense as a snippet source. The set is
// intentionally narrow: file types where the body is human-readable
// without an external decoder. PDF / office / epub need a real text
// extractor and are out of scope for v1; binary families are obviously
// not text.
func isTextContentType(name string) bool {
	switch name {
	case "markdown", "text", "html", "csv", "json", "xml":
		return true
	}
	// Source code (Go, Python, JS, …) — all 18 languages are text.
	return strings.HasPrefix(name, "source/")
}

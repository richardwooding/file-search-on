package celexpr

import (
	"context"
	"io"
	"io/fs"
	"strings"
)

// isTextForBody reports whether the given content type's body is
// readable as text and worth surfacing for CEL body-content filters.
// Kept in sync with the snippet reader's text-type allowlist
// (internal/search/snippet.go).
func isTextForBody(name string) bool {
	switch name {
	case "markdown", "text", "html", "csv", "json", "xml":
		return true
	}
	return strings.HasPrefix(name, "source/")
}

// readBody returns the file's contents capped at maxBytes. When
// maxBytes <= 0 the package default (1 MiB) is used. The cap is a
// hard limit: files larger than the cap are silently truncated, not
// rejected — agents writing `body.contains("X")` filters typically
// want the prefix to participate even if the file is enormous.
//
// ctx is checked at entry; long reads will surface ctx.Err() through
// the underlying file IO eventually but we don't poll between bytes.
func readBody(ctx context.Context, fsys fs.FS, fsPath string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = defaultBodyMaxBytes
	}
	f, err := fsys.Open(fsPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	// LimitReader caps the underlying read; ReadAll then collects
	// up to that many bytes. We add 1 to the cap so we can detect
	// "truncated" if we ever want to (we don't currently surface
	// that distinction — see the doc above).
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

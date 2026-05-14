package celexpr

import (
	"context"
	"io"
	"io/fs"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// isTextForBody reports whether the given content type's body is
// readable as plain text and worth surfacing for CEL body-content
// filters via a raw byte read. Kept in sync with the snippet reader's
// text-type allowlist (internal/search/snippet.go).
//
// Structured-document types (office/* and epub) ALSO populate body
// but via a format-specific extractor — see isStructuredBody +
// content.ExtractBody. The two checks are split because the read
// path is fundamentally different (raw byte slice vs ZIP-walking
// XML extractor) and only the plain-text path supports the streaming
// LimitReader semantics.
func isTextForBody(name string) bool {
	switch name {
	case "markdown", "text", "html", "csv", "json", "xml":
		return true
	}
	return strings.HasPrefix(name, "source/")
}

// isStructuredBody reports whether the given content type's body is
// best surfaced via a format-specific extractor rather than a raw byte
// read. Office documents (DOCX / XLSX / PPTX / ODT) and EPUB are ZIP
// envelopes with body text buried in XML; .eml / .mbox are RFC 5322
// messages with the body buried under MIME headers + transfer-encoding
// + multipart boundaries. Agents searching these files want the
// human-readable text, not the wire envelope. Routed through
// content.ExtractBody at read time.
func isStructuredBody(name string) bool {
	switch name {
	case "office/docx", "office/xlsx", "office/pptx", "office/odt",
		"epub",
		"email/rfc822", "email/mbox":
		return true
	}
	return false
}

// canExtractBody reports whether the body CEL variable can be
// populated for this content type — either as raw text (isTextForBody)
// or via a format-specific extractor (isStructuredBody). The walker
// uses this to gate the body read.
func canExtractBody(name string) bool {
	return isTextForBody(name) || isStructuredBody(name)
}

// readBody returns the file's body as a string capped at maxBytes.
// When maxBytes <= 0 the package default (1 MiB) is used. The cap is a
// hard limit: files larger than the cap are silently truncated, not
// rejected — agents writing `body.contains("X")` filters typically
// want the prefix to participate even if the file is enormous.
//
// Dispatch:
//   - text-shaped types (markdown / text / html / csv / json / xml /
//     source/*) read raw bytes via io.LimitReader.
//   - structured types (office/* / epub) call content.ExtractBody for
//     a format-specific extractor that strips XML / ZIP envelope and
//     returns the human-readable text only.
//
// ctx is checked at entry; structured extractors honour ctx between
// every XML token internally. Raw reads will surface ctx.Err()
// through the underlying file IO eventually but don't poll between
// bytes.
func readBody(ctx context.Context, fsys fs.FS, fsPath, contentTypeName string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = defaultBodyMaxBytes
	}
	if isStructuredBody(contentTypeName) {
		return content.ExtractBody(ctx, contentTypeName, fsys, fsPath, maxBytes)
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

package content

import (
	"context"
	"io/fs"
)

// ExtractBody returns the human-readable text body of a structured-
// document content type — OOXML office formats (DOCX / XLSX / PPTX),
// ODT, EPUB, email (.eml / .mbox), and PDF. For everything else it
// returns "" and a nil error; the caller should fall through to its
// existing text-file body reader.
//
// Output is paragraph-joined plain text (newline-separated). XML
// formatting / styling / metadata are stripped; what remains is what
// a CEL filter like body.contains("transformer") or body.matches(...)
// can search. Capped at maxBytes (0 means use the existing 1 MiB
// default the caller picks). Honours ctx between every XML token.
//
// Used by the celexpr body reader (internal/celexpr/body.go) when the
// caller opts in via IncludeBody on a structured-document file. Kept
// in this package because the extractors share the ZIP / Dublin Core
// scaffolding already used for metadata extraction.
//
// Per-format extractors live in body_pdf.go / body_office.go /
// body_epub.go / body_email.go. Shared XML and HTML text-strip
// helpers live in body_shared.go.
func ExtractBody(ctx context.Context, contentTypeName string, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch contentTypeName {
	case "office/docx":
		return ooxmlBody(ctx, fsys, filePath, []string{"word/document.xml"}, "p", "t", maxBytes)
	case "office/xlsx":
		return xlsxBody(ctx, fsys, filePath, maxBytes)
	case "office/pptx":
		return pptxBody(ctx, fsys, filePath, maxBytes)
	case "office/odt":
		return odtBody(ctx, fsys, filePath, maxBytes)
	case "epub":
		return epubBody(ctx, fsys, filePath, maxBytes)
	case "email/rfc822":
		return emlBody(ctx, fsys, filePath, maxBytes)
	case "email/mbox":
		return mboxBody(ctx, fsys, filePath, maxBytes)
	case "pdf":
		return pdfBody(ctx, fsys, filePath, maxBytes)
	case "browser/bookmarks-chromium":
		return chromiumBookmarksBody(ctx, fsys, filePath, maxBytes)
	case "browser/bookmarks-safari":
		return safariBookmarksBody(ctx, fsys, filePath, maxBytes)
	}
	return "", nil
}

// ExtractBodyOSPath is the sibling entry point for content types whose
// extractor cannot read through fs.FS — today only `database/sqlite`,
// because the modernc.org/sqlite driver requires an OS path to open
// the file. Archive-walk paths (where the file lives inside a ZIP /
// TAR) cannot reach this code; callers MUST gate on the file having a
// real on-disk path before invoking.
//
// Returns "" + nil error for unknown content types (matches the
// ExtractBody contract — caller falls through to its existing
// behaviour).
func ExtractBodyOSPath(ctx context.Context, contentTypeName, osPath string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch contentTypeName {
	case "database/sqlite":
		return sqliteBody(ctx, osPath, maxBytes)
	}
	return "", nil
}

// RequiresOSPath reports whether the given content type's body
// extractor needs the real OS path (cannot operate through fs.FS).
// Callers use this to gate body extraction on archive-walk paths.
func RequiresOSPath(contentTypeName string) bool {
	return contentTypeName == "database/sqlite"
}

package content_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

const ooxmlCoreXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties
    xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:dcterms="http://purl.org/dc/terms/"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>Quarterly Report</dc:title>
  <dc:creator>R. Wooding</dc:creator>
  <dc:language>en-GB</dc:language>
</cp:coreProperties>`

const odtMetaXML = `<?xml version="1.0" encoding="UTF-8"?>
<office:document-meta xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
                      xmlns:dc="http://purl.org/dc/elements/1.1/">
  <office:meta>
    <dc:title>Open Doc Title</dc:title>
    <dc:creator>Open Author</dc:creator>
    <dc:language>fr</dc:language>
  </office:meta>
</office:document-meta>`

func writeOOXMLZip(t *testing.T, path string) {
	t.Helper()
	writeZipWithEntries(t, path, map[string]string{
		"docProps/core.xml":   ooxmlCoreXML,
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
	})
}

func writeODTZip(t *testing.T, path string) {
	t.Helper()
	writeZipWithEntries(t, path, map[string]string{
		"meta.xml":   odtMetaXML,
		"mimetype":   "application/vnd.oasis.opendocument.text",
		"manifest.rdf": `<?xml version="1.0"?><x/>`,
	})
}

func writeZipWithEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOOXMLAttributes(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		ext  string
		want string
	}{
		{".docx", "office/docx"},
		{".xlsx", "office/xlsx"},
		{".pptx", "office/pptx"},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			path := filepath.Join(dir, "doc"+tc.ext)
			writeOOXMLZip(t, path)
			ct := content.DefaultRegistry().Detect(path)
			if ct == nil || ct.Name() != tc.want {
				t.Fatalf("Detect: got %v, want %s", ct, tc.want)
			}
			attrs, err := ct.Attributes(t.Context(), path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["title"]; got != "Quarterly Report" {
				t.Errorf("title = %q, want \"Quarterly Report\"", got)
			}
			if got := attrs["author"]; got != "R. Wooding" {
				t.Errorf("author = %q, want \"R. Wooding\"", got)
			}
			if got := attrs["language"]; got != "en-GB" {
				t.Errorf("language = %q, want \"en-GB\"", got)
			}
		})
	}
}

func TestODTAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open.odt")
	writeODTZip(t, path)

	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "office/odt" {
		t.Fatalf("Detect: got %v, want office/odt", ct)
	}

	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["title"]; got != "Open Doc Title" {
		t.Errorf("title = %q, want \"Open Doc Title\"", got)
	}
	if got := attrs["author"]; got != "Open Author" {
		t.Errorf("author = %q, want \"Open Author\"", got)
	}
	if got := attrs["language"]; got != "fr" {
		t.Errorf("language = %q, want \"fr\"", got)
	}
}

func TestOfficeMissingMetadata(t *testing.T) {
	// A docx without docProps/core.xml: zero values, no error.
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.docx")
	writeZipWithEntries(t, path, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
	})
	ct := content.DefaultRegistry().Detect(path)
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["language"]; got != "" {
		t.Errorf("language with missing core.xml = %q, want empty", got)
	}
	if _, ok := attrs["title"]; ok {
		t.Errorf("title should be absent when core.xml is missing")
	}
}

func TestOfficeNotAZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.docx")
	if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	if _, err := ct.Attributes(t.Context(), path); err == nil {
		t.Errorf("Attributes on broken zip: expected error, got nil")
	}
}

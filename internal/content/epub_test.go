package content_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

const containerXML = `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const opfXML = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Hitchhiker's Guide</dc:title>
    <dc:creator>Douglas Adams</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
</package>`

func writeMinimalEPUB(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	for name, body := range map[string]string{
		"META-INF/container.xml": containerXML,
		"OEBPS/content.opf":      opfXML,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEPUBAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guide.epub")
	writeMinimalEPUB(t, path)

	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "epub" {
		t.Fatalf("Detect: got %v, want epub", ct)
	}

	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["title"]; got != "The Hitchhiker's Guide" {
		t.Errorf("title = %q, want \"The Hitchhiker's Guide\"", got)
	}
	if got := attrs["author"]; got != "Douglas Adams" {
		t.Errorf("author = %q, want \"Douglas Adams\"", got)
	}
	if got := attrs["language"]; got != "en" {
		t.Errorf("language = %q, want \"en\"", got)
	}
}

func TestEPUBNotAZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.epub")
	if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "epub" {
		t.Fatalf("Detect: got %v, want epub", ct)
	}
	if _, err := ct.Attributes(t.Context(), path); err == nil {
		t.Errorf("Attributes on broken zip: expected error, got nil")
	}
}

package content_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
	"testing/fstest"

	"github.com/richardwooding/file-search-on/internal/content"
)

// buildSampleZip returns the bytes of a small ZIP archive with three
// entries. unifiedRoot=true puts them under a single top-level dir
// "sample/" so has_root_dir=true; false sprawls them under different
// top-levels.
func buildSampleZip(t *testing.T, unifiedRoot bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	prefix := "sample/"
	if !unifiedRoot {
		prefix = ""
	}
	for _, e := range []struct{ name, body string }{
		{"README.txt", "hello"},
		{"data.txt", "x"},
		{"more.txt", "y"},
	} {
		f, err := w.Create(prefix + e.name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := f.Write([]byte(e.body)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// buildSampleTar returns the bytes of a small TAR archive. gzipped=true
// wraps the output in gzip for the tar.gz path.
func buildSampleTar(t *testing.T, unifiedRoot, gzipped bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	var tw *tar.Writer
	var gz *gzip.Writer
	if gzipped {
		gz = gzip.NewWriter(&buf)
		tw = tar.NewWriter(gz)
	} else {
		tw = tar.NewWriter(&buf)
	}
	prefix := "sample/"
	if !unifiedRoot {
		prefix = ""
	}
	for _, e := range []struct{ name, body string }{
		{"README.txt", "hello"},
		{"data.txt", "x"},
		{"more.txt", "y"},
	} {
		hdr := &tar.Header{
			Name: prefix + e.name,
			Mode: 0o644,
			Size: int64(len(e.body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
	}
	return buf.Bytes()
}

func buildStandaloneGzip(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestArchiveZIP_UnifiedRoot(t *testing.T) {
	fsys := fstest.MapFS{"a.zip": {Data: buildSampleZip(t, true)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.zip")
	if ct.Name() != "archive/zip" {
		t.Fatalf("Detect = %q; want archive/zip", ct.Name())
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.zip")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if ec, _ := a["entry_count"].(int64); ec != 3 {
		t.Errorf("entry_count = %v; want 3", a["entry_count"])
	}
	if us, _ := a["uncompressed_size"].(int64); us != int64(len("hello")+len("x")+len("y")) {
		t.Errorf("uncompressed_size = %v; want 7", a["uncompressed_size"])
	}
	if rd, _ := a["has_root_dir"].(bool); !rd {
		t.Errorf("has_root_dir = false; want true")
	}
	tops, _ := a["top_level_entries"].([]string)
	if len(tops) != 1 || tops[0] != "sample" {
		t.Errorf("top_level_entries = %v; want [sample]", tops)
	}
}

func TestArchiveZIP_SprawlingRoot(t *testing.T) {
	fsys := fstest.MapFS{"a.zip": {Data: buildSampleZip(t, false)}}
	a, err := content.DefaultRegistry().Detect(fsys, "a.zip").Attributes(t.Context(), fsys, "a.zip")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if rd, _ := a["has_root_dir"].(bool); rd {
		t.Errorf("has_root_dir = true; want false (3 sprawling top-level entries)")
	}
	tops, _ := a["top_level_entries"].([]string)
	if len(tops) != 3 {
		t.Errorf("top_level_entries = %v; want 3 entries", tops)
	}
}

func TestArchiveTAR(t *testing.T) {
	fsys := fstest.MapFS{"a.tar": {Data: buildSampleTar(t, true, false)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.tar")
	if ct.Name() != "archive/tar" {
		t.Fatalf("Detect = %q; want archive/tar", ct.Name())
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.tar")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if ec, _ := a["entry_count"].(int64); ec != 3 {
		t.Errorf("entry_count = %v; want 3", a["entry_count"])
	}
	if rd, _ := a["has_root_dir"].(bool); !rd {
		t.Errorf("has_root_dir = false; want true")
	}
}

func TestArchiveTARGZ(t *testing.T) {
	fsys := fstest.MapFS{"a.tar.gz": {Data: buildSampleTar(t, true, true)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.tar.gz")
	if ct.Name() != "archive/tar+gzip" {
		t.Fatalf("Detect = %q; want archive/tar+gzip", ct.Name())
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.tar.gz")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if ec, _ := a["entry_count"].(int64); ec != 3 {
		t.Errorf("entry_count = %v; want 3 (entries inside the tar)", a["entry_count"])
	}
}

func TestArchiveTGZ_AlsoMatchesTarGzip(t *testing.T) {
	// .tgz extension is registered alongside .tar.gz; same parser.
	fsys := fstest.MapFS{"a.tgz": {Data: buildSampleTar(t, true, true)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.tgz")
	if ct.Name() != "archive/tar+gzip" {
		t.Fatalf("Detect = %q; want archive/tar+gzip", ct.Name())
	}
}

func TestArchiveGZIP_Standalone(t *testing.T) {
	body := []byte("Hello, this is a test payload that the gzip footer's ISIZE will record.")
	fsys := fstest.MapFS{"a.gz": {Data: buildStandaloneGzip(t, body)}}
	ct := content.DefaultRegistry().Detect(fsys, "a.gz")
	if ct.Name() != "archive/gzip" {
		t.Fatalf("Detect = %q; want archive/gzip", ct.Name())
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.gz")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if ec, _ := a["entry_count"].(int64); ec != 1 {
		t.Errorf("entry_count = %v; want 1 (gzip = single stream)", a["entry_count"])
	}
	if us, _ := a["uncompressed_size"].(int64); us != int64(len(body)) {
		t.Errorf("uncompressed_size = %v; want %d (ISIZE footer)", a["uncompressed_size"], len(body))
	}
	if rd, _ := a["has_root_dir"].(bool); rd {
		t.Errorf("has_root_dir = true; want false (standalone gzip has no directory structure)")
	}
}


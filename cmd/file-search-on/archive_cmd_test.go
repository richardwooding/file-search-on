package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeZip builds a small ZIP archive at dest with the given entry map.
// Returns the absolute path. Used as the fixture for archive tests.
func makeZip(t *testing.T, dest string, entries map[string]string) string {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return dest
}

// TestArchiveContentsCmd_Run_ListsAllEntries seeds a 3-entry ZIP
// and confirms the JSON output reports all three.
func TestArchiveContentsCmd_Run_ListsAllEntries(t *testing.T) {
	zipPath := makeZip(t, filepath.Join(t.TempDir(), "test.zip"), map[string]string{
		"README.md":      "# hello\n",
		"src/main.go":    "package main\n\nfunc main(){}\n",
		"data/config.json": `{"k":"v"}`,
	})

	cmd := &ArchiveContentsCmd{Archive: zipPath, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	entries, _ := got["Entries"].([]any)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d: %v", len(entries), entries)
	}
}

// TestArchiveContentsCmd_Run_GlobPrune applies the cheap basename
// pre-prune and confirms only matching entries come back.
func TestArchiveContentsCmd_Run_GlobPrune(t *testing.T) {
	zipPath := makeZip(t, filepath.Join(t.TempDir(), "test.zip"), map[string]string{
		"a.go":   "package main\n",
		"b.go":   "package other\n",
		"c.txt":  "not Go\n",
		"d.json": `{}`,
	})

	cmd := &ArchiveContentsCmd{Archive: zipPath, Glob: "*.go", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	entries, _ := got["Entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after *.go glob prune, got %d: %v", len(entries), entries)
	}
}

// TestArchiveContentsCmd_Run_MaxEntries caps the result count.
func TestArchiveContentsCmd_Run_MaxEntries(t *testing.T) {
	zipPath := makeZip(t, filepath.Join(t.TempDir(), "test.zip"), map[string]string{
		"a.txt": "1\n", "b.txt": "2\n", "c.txt": "3\n", "d.txt": "4\n", "e.txt": "5\n",
	})

	cmd := &ArchiveContentsCmd{Archive: zipPath, MaxEntries: 2, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	entries, _ := got["Entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after --max=2, got %d", len(entries))
	}
}

// TestArchiveReadCmd_Run_ExtractsEntry reads a specific entry's
// bytes back out via the JSON envelope path.
func TestArchiveReadCmd_Run_ExtractsEntry(t *testing.T) {
	const wantContent = "# README\n\nhi there\n"
	zipPath := makeZip(t, filepath.Join(t.TempDir(), "test.zip"), map[string]string{
		"README.md":   wantContent,
		"src/main.go": "package main\n",
	})

	cmd := &ArchiveReadCmd{Archive: zipPath, Entry: "README.md", Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	// Go's json encoder base64-encodes []byte by default; decode to
	// compare against the original entry body.
	contentB64, _ := got["content"].(string)
	decoded, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		t.Fatalf("base64 decode content: %v (raw: %q)", err, contentB64)
	}
	if string(decoded) != wantContent {
		t.Errorf("content = %q, want %q", decoded, wantContent)
	}
}

// TestArchiveReadCmd_Run_MissingEntry confirms an unknown entry
// surfaces the documented exit-1 error.
func TestArchiveReadCmd_Run_MissingEntry(t *testing.T) {
	zipPath := makeZip(t, filepath.Join(t.TempDir(), "test.zip"), map[string]string{"only.txt": "x\n"})

	cmd := &ArchiveReadCmd{Archive: zipPath, Entry: "does-not-exist.md", Output: "json"}
	_, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatalf("expected error for missing entry, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

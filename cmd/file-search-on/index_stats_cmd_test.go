package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// warmTestIndex opens a fresh on-disk index at path, walks root to
// populate it, and closes it — leaving a persisted cache for index-stats
// to inspect in a separate process-like open.
func warmTestIndex(t *testing.T, path, root string) {
	t.Helper()
	idx, err := index.OpenWith(path, index.BodyCacheCap{})
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := search.Walk(t.Context(), search.Options{
		Roots: []string{root},
		Expr:  "true",
		Index: idx,
	}, contentpkg.DefaultRegistry()); err != nil {
		t.Fatalf("warm walk: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("close index: %v", err)
	}
}

// TestIndexStatsCmd_Text confirms a warmed on-disk index reports a
// non-zero attr-entries count and names the persistent backend.
func TestIndexStatsCmd_Text(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.md"), "# title\n\nbody\n")
	mustWriteFile(t, filepath.Join(tmp, "b.json"), `{"k":"v"}`)
	idxPath := filepath.Join(tmp, "cache.db")
	warmTestIndex(t, idxPath, tmp)

	cmd := &IndexStatsCmd{IndexPath: idxPath, Output: "text"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "persistent") {
		t.Errorf("expected persistent backend, got: %q", out)
	}
	if !strings.Contains(out, "attr entries") {
		t.Errorf("expected attr-entries line, got: %q", out)
	}
}

// TestIndexStatsCmd_JSON checks the JSON shape and that a warmed index
// reports at least one attribute entry.
func TestIndexStatsCmd_JSON(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.md"), "# title\n\nbody\n")
	idxPath := filepath.Join(tmp, "cache.db")
	warmTestIndex(t, idxPath, tmp)

	cmd := &IndexStatsCmd{IndexPath: idxPath, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got struct {
		Backend          string `json:"backend"`
		AttrEntriesCount uint64 `json:"attr_entries_count"`
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	if got.Backend != "persistent" {
		t.Errorf("expected persistent backend, got %q", got.Backend)
	}
	if got.AttrEntriesCount == 0 {
		t.Errorf("expected at least one cached attr entry, got 0; raw: %q", out)
	}
}

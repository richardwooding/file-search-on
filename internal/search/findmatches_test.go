package search_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// writeFile is a small helper for the find-matches tests. The existing
// writeNumberedLines helper in readlines_test.go writes "line N\n"
// rows which aren't great for regex matching; tests below build their
// own content.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestFindMatches_SingleHitNoContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc main() {\n\t// TODO: ship it\n}\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_source",
		Pattern: "TODO",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1; matches=%+v", res.Count, res.Matches)
	}
	m := res.Matches[0]
	if m.Line != 4 {
		t.Errorf("Line=%d want 4", m.Line)
	}
	if !strings.Contains(m.Text, "TODO") {
		t.Errorf("Text=%q lacks TODO", m.Text)
	}
	if len(m.Before) != 0 || len(m.After) != 0 {
		t.Errorf("expected no context windows, got before=%v after=%v", m.Before, m.After)
	}
	if m.ContentType != "source/go" {
		t.Errorf("ContentType=%q want source/go", m.ContentType)
	}
	if res.FilesScanned == 0 {
		t.Errorf("FilesScanned=0 want >0")
	}
	if res.FilesWithMatches != 1 {
		t.Errorf("FilesWithMatches=%d want 1", res.FilesWithMatches)
	}
}

func TestFindMatches_ContextWindows(t *testing.T) {
	dir := t.TempDir()
	// Match on line 4. before=2 → lines 2,3; after=2 → lines 5,6.
	writeFile(t, dir, "a.go", "// line1\n// line2\n// line3\n// MATCH HERE\n// line5\n// line6\n// line7\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:          dir,
		Expr:          "is_source",
		Pattern:       "MATCH",
		ContextBefore: 2,
		ContextAfter:  2,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1", res.Count)
	}
	m := res.Matches[0]
	if m.Line != 4 {
		t.Errorf("Line=%d want 4", m.Line)
	}
	wantBefore := []string{"// line2", "// line3"}
	if !equalStrings(m.Before, wantBefore) {
		t.Errorf("Before=%v want %v", m.Before, wantBefore)
	}
	wantAfter := []string{"// line5", "// line6"}
	if !equalStrings(m.After, wantAfter) {
		t.Errorf("After=%v want %v", m.After, wantAfter)
	}
}

func TestFindMatches_ContextAtFileBoundaries(t *testing.T) {
	dir := t.TempDir()
	// Match on line 1 → no Before. Match on last line → truncated After.
	writeFile(t, dir, "a.go", "TODO start\nline2\nline3\nTODO end\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:          dir,
		Expr:          "is_source",
		Pattern:       "TODO",
		ContextBefore: 3,
		ContextAfter:  3,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 2 {
		t.Fatalf("Count=%d want 2", res.Count)
	}
	// Matches are sorted by (path, line) — same path, so line order.
	first := res.Matches[0]
	if first.Line != 1 || len(first.Before) != 0 {
		t.Errorf("first match line=%d before=%v want line=1 before=[]", first.Line, first.Before)
	}
	if !equalStrings(first.After, []string{"line2", "line3", "TODO end"}) {
		t.Errorf("first.After=%v", first.After)
	}
	last := res.Matches[1]
	if last.Line != 4 {
		t.Errorf("last match line=%d want 4", last.Line)
	}
	// "TODO start" is line 1 — exactly 3 lines before "TODO end" on
	// line 4 — so with ContextBefore=3 it appears in last.Before. The
	// before-ring includes all prior lines regardless of whether
	// they themselves matched.
	if !equalStrings(last.Before, []string{"TODO start", "line2", "line3"}) {
		t.Errorf("last.Before=%v want [TODO start, line2, line3]", last.Before)
	}
	if len(last.After) != 0 {
		t.Errorf("last.After=%v want empty (match is at last line)", last.After)
	}
}

func TestFindMatches_MultipleFilesSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "z.go", "TODO in z\n")
	writeFile(t, dir, "a.go", "TODO in a\n")
	writeFile(t, dir, "m.go", "TODO in m\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_source",
		Pattern: "TODO",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 3 {
		t.Fatalf("Count=%d want 3", res.Count)
	}
	// Matches sorted by path: a < m < z.
	gotPaths := []string{
		filepath.Base(res.Matches[0].Path),
		filepath.Base(res.Matches[1].Path),
		filepath.Base(res.Matches[2].Path),
	}
	wantPaths := []string{"a.go", "m.go", "z.go"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Errorf("paths=%v want %v", gotPaths, wantPaths)
	}
}

func TestFindMatches_MaxMatchesPerFile(t *testing.T) {
	dir := t.TempDir()
	// 5 TODO hits.
	writeFile(t, dir, "a.go", "TODO 1\nfoo\nTODO 2\nbar\nTODO 3\nbaz\nTODO 4\nqux\nTODO 5\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:              dir,
		Expr:              "is_source",
		Pattern:           "TODO",
		MaxMatchesPerFile: 2,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 2 {
		t.Fatalf("Count=%d want 2 (capped per file)", res.Count)
	}
	if res.Matches[0].Line != 1 || res.Matches[1].Line != 3 {
		t.Errorf("lines=%d,%d want 1,3", res.Matches[0].Line, res.Matches[1].Line)
	}
}

func TestFindMatches_CapKeepsFillingAfterWindow(t *testing.T) {
	dir := t.TempDir()
	// Match on line 1; cap is 1; After window is 3 lines → last 3 lines
	// must be filled even though we hit the cap on line 1.
	writeFile(t, dir, "a.go", "TODO\nafter1\nafter2\nafter3\nignored\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:              dir,
		Expr:              "is_source",
		Pattern:           "TODO",
		ContextAfter:      3,
		MaxMatchesPerFile: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1", res.Count)
	}
	if !equalStrings(res.Matches[0].After, []string{"after1", "after2", "after3"}) {
		t.Errorf("After=%v want full after window despite cap", res.Matches[0].After)
	}
}

func TestFindMatches_BinariesSkipped(t *testing.T) {
	dir := t.TempDir()
	// Real binary content type: a PNG signature + arbitrary bytes that
	// happen to spell "TODO". The detector picks image/png by magic,
	// which is NOT a text content type and so should be filtered.
	writeFile(t, dir, "a.png", "\x89PNG\r\n\x1a\nIDATTODOmoredata")
	writeFile(t, dir, "b.go", "// TODO real source\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:    dir,
		Pattern: "TODO",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1 (binary should be skipped); matches=%+v", res.Count, res.Matches)
	}
	if !strings.HasSuffix(res.Matches[0].Path, "b.go") {
		t.Errorf("matched path=%q want b.go", res.Matches[0].Path)
	}
}

func TestFindMatches_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "anything\n")

	_, err := search.FindMatches(t.Context(), search.Options{
		Root: dir,
		Expr: "is_source",
	}, content.DefaultRegistry())
	if !errors.Is(err, search.ErrEmptyPattern) {
		t.Fatalf("err=%v want ErrEmptyPattern", err)
	}
}

func TestFindMatches_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "anything\n")

	_, err := search.FindMatches(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_source",
		Pattern: "(unclosed",
	}, content.DefaultRegistry())
	if err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	if !strings.Contains(err.Error(), "compile pattern") {
		t.Errorf("err=%q want compile-pattern wrapper", err)
	}
}

func TestFindMatches_CancelledMidScan(t *testing.T) {
	dir := t.TempDir()
	for i := range 50 {
		writeFile(t, dir, "f"+itoa(i)+".go", "// TODO repeated\n// TODO again\n")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	res, err := search.FindMatches(ctx, search.Options{
		Root:    dir,
		Expr:    "is_source",
		Pattern: "TODO",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches returned err=%v (cancellation should surface via Cancelled flag)", err)
	}
	if !res.Cancelled {
		t.Errorf("Cancelled=false; want true (ctx was cancelled before scan)")
	}
	if res.CancellationReason != "client_cancel" {
		t.Errorf("CancellationReason=%q want client_cancel", res.CancellationReason)
	}
}

func TestFindMatches_CELFilterPrunes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "// TODO go\n")
	writeFile(t, dir, "b.py", "# TODO py\n")

	// Only Go sources.
	res, err := search.FindMatches(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_source && language == \"go\"",
		Pattern: "TODO",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1", res.Count)
	}
	if !strings.HasSuffix(res.Matches[0].Path, "a.go") {
		t.Errorf("matched %q want a.go", res.Matches[0].Path)
	}
}

func TestFindMatches_ExcludesPrune(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "vendor", "deep"), "v.go", "// TODO vendor\n")
	writeFile(t, dir, "main.go", "// TODO main\n")

	res, err := search.FindMatches(t.Context(), search.Options{
		Root:     dir,
		Expr:     "is_source",
		Pattern:  "TODO",
		Excludes: []string{"vendor"},
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1 (vendor pruned)", res.Count)
	}
	if !strings.HasSuffix(res.Matches[0].Path, "main.go") {
		t.Errorf("matched %q want main.go", res.Matches[0].Path)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

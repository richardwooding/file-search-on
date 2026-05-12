package search_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func writeNumberedLines(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(itoa(i))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestReadLines_FullRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 5)
	res, err := search.ReadLines(t.Context(), os.DirFS(dir), "f.txt", 0, 0, 0)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(res.Lines) != 5 {
		t.Fatalf("len(Lines)=%d want 5", len(res.Lines))
	}
	if res.TotalLines != 5 {
		t.Errorf("TotalLines=%d want 5", res.TotalLines)
	}
	if res.Lines[0] != "line 1" || res.Lines[4] != "line 5" {
		t.Errorf("got %v", res.Lines)
	}
	if res.Truncated {
		t.Errorf("Truncated unexpectedly")
	}
}

func TestReadLines_MidRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 10)
	res, err := search.ReadLines(t.Context(), os.DirFS(dir), "f.txt", 3, 6, 0)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	want := []string{"line 3", "line 4", "line 5", "line 6"}
	if len(res.Lines) != 4 {
		t.Fatalf("len(Lines)=%d want 4", len(res.Lines))
	}
	for i, w := range want {
		if res.Lines[i] != w {
			t.Errorf("Lines[%d]=%q want %q", i, res.Lines[i], w)
		}
	}
	if res.TotalLines != 10 {
		t.Errorf("TotalLines=%d want 10", res.TotalLines)
	}
	if res.StartLine != 3 || res.EndLine != 6 {
		t.Errorf("StartLine=%d EndLine=%d want 3 6", res.StartLine, res.EndLine)
	}
}

func TestReadLines_EndPastEOF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 5)
	res, err := search.ReadLines(t.Context(), os.DirFS(dir), "f.txt", 3, 100, 0)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(res.Lines) != 3 { // lines 3, 4, 5
		t.Errorf("len(Lines)=%d want 3", len(res.Lines))
	}
	if res.EndLine != 5 {
		t.Errorf("EndLine=%d want 5 (clamped to TotalLines)", res.EndLine)
	}
}

func TestReadLines_MaxLinesTruncates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 50)
	res, err := search.ReadLines(t.Context(), os.DirFS(dir), "f.txt", 0, 0, 10)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if !res.Truncated {
		t.Errorf("Truncated=false; want true")
	}
	if len(res.Lines) != 10 {
		t.Errorf("len(Lines)=%d want 10", len(res.Lines))
	}
	if res.TotalLines != 50 {
		t.Errorf("TotalLines=%d want 50", res.TotalLines)
	}
}

func TestReadLines_InvalidRangeError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 5)
	_, err := search.ReadLines(t.Context(), os.DirFS(dir), "f.txt", 10, 3, 0)
	if err == nil {
		t.Fatal("expected ErrInvalidLineRange when start > end")
	}
}

func TestReadLines_CtxCancelled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	writeNumberedLines(t, p, 5)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := search.ReadLines(ctx, os.DirFS(dir), "f.txt", 0, 0, 0)
	if err == nil {
		t.Fatal("expected ctx.Canceled-derived error")
	}
}

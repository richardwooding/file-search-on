package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestCoverageGaps(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(dir, "foo.go"), "package m\n\n"+ // 1,2
		"func covered() int {\n"+ // 3
		"\tx := 1\n"+ // 4
		"\treturn x\n"+ // 5
		"}\n"+ // 6
		"func uncovered() int {\n"+ // 7
		"\ty := 2\n"+ // 8
		"\treturn y\n"+ // 9
		"}\n") // 10
	// covered() block runs (count 1); uncovered() block never runs (count 0).
	profile := "mode: set\n" +
		"example.com/m/foo.go:3.20,6.2 2 1\n" +
		"example.com/m/foo.go:7.22,10.2 2 0\n"
	profPath := filepath.Join(dir, "cov.out")
	mustWriteFile(t, profPath, profile)

	res, err := search.CoverageGaps(t.Context(), profPath, dir, 1.0, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("CoverageGaps: %v", err)
	}
	if res.ProfileMode != "set" {
		t.Errorf("ProfileMode = %q, want set", res.ProfileMode)
	}
	if res.FilesAnalysed != 1 {
		t.Errorf("FilesAnalysed = %d, want 1", res.FilesAnalysed)
	}
	if res.Count != 1 {
		t.Fatalf("Count = %d, want 1 (only uncovered()): %+v", res.Count, res.Gaps)
	}
	g := res.Gaps[0]
	if g.Function != "uncovered" || !g.FullyUncovered {
		t.Errorf("gap = %+v, want function 'uncovered', fully uncovered", g)
	}
	if g.CoveredStatements != 0 || g.TotalStatements != 2 {
		t.Errorf("statements = %d/%d, want 0/2", g.CoveredStatements, g.TotalStatements)
	}
	if g.Path != "foo.go" {
		t.Errorf("path = %q, want foo.go (module-relative)", g.Path)
	}
}

func TestCoverageGaps_ThresholdFiltersPartial(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(dir, "p.go"), "package m\n\n"+
		"func half() int {\n"+ // 3
		"\ta := 1\n"+ // 4
		"\tb := 2\n"+ // 5
		"\treturn a + b\n"+ // 6
		"}\n") // 7
	// Two blocks in half(): one covered, one not → 50%.
	profile := "mode: set\n" +
		"example.com/m/p.go:3.18,4.8 1 1\n" +
		"example.com/m/p.go:5.2,6.14 1 0\n"
	profPath := filepath.Join(dir, "c.out")
	mustWriteFile(t, profPath, profile)

	// threshold 1.0 → 50% function is a gap.
	res, _ := search.CoverageGaps(t.Context(), profPath, dir, 1.0, content.DefaultRegistry())
	if res.Count != 1 || res.Gaps[0].CoveredPct != 0.5 {
		t.Errorf("threshold 1.0: want 1 gap at 50%%, got %+v", res.Gaps)
	}
	// threshold 0.5 → 50% is NOT strictly below 0.5, so no gap.
	res2, _ := search.CoverageGaps(t.Context(), profPath, dir, 0.5, content.DefaultRegistry())
	if res2.Count != 0 {
		t.Errorf("threshold 0.5: a 50%%-covered function should not be < 0.5, got %+v", res2.Gaps)
	}
}

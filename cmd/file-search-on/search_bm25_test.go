package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchCmd_KeywordQueryRanksBuffered is the regression for the two
// CLI wiring bugs found while building #335: --keyword-query must (1)
// force buffered mode even in a streaming-friendly output mode like
// bare, and (2) NOT be clobbered by the default path-sort. The
// term-dense file must lead the bare output.
func TestSearchCmd_KeywordQueryRanksBuffered(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "dense.md"), "# k\n\n"+strings.Repeat("kubernetes scheduler kubernetes ", 20))
	mustWriteFile(t, filepath.Join(dir, "mention.md"), "# m\n\none kubernetes mention in otherwise unrelated prose here ok\n")
	mustWriteFile(t, filepath.Join(dir, "zzz_unrelated.md"), "# z\n\n"+strings.Repeat("flour sugar butter ", 20))

	cmd := &SearchCmd{
		Dir:          []string{dir},
		Expr:         "is_markdown",
		KeywordQuery: "kubernetes scheduler",
		Output:       "bare", // streaming-friendly mode — must still buffer for BM25
		NoIndex:      true,
	}
	stdout, _ := runCapturingBoth(t, func() error { return cmd.Run(t.Context()) })

	lines := []string{}
	for l := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, filepath.Base(l))
		}
	}
	if len(lines) < 2 {
		t.Fatalf("expected multiple matches, got %v", lines)
	}
	// dense.md must rank first (highest BM25), not alphabetical/walk order
	// (zzz_unrelated.md is intentionally named to lose a path-sort).
	if lines[0] != "dense.md" {
		t.Errorf("keyword-query ranking: want dense.md first, got order %v", lines)
	}
}

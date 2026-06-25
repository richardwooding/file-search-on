package search_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/gitmeta"

	"github.com/richardwooding/file-search-on/internal/search"
)

// branchyFunc returns a Go source file whose sole function has cyclomatic
// complexity well above the default gate (20 if-branches → ~21).
func branchyFunc(pkg string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nfunc Branchy(x int) int {\n\tr := 0\n")
	for i := 1; i <= 20; i++ {
		b.WriteString("\tif x > ")
		b.WriteByte(byte('0' + i%10))
		b.WriteString(" {\n\t\tr++\n\t}\n")
	}
	b.WriteString("\treturn r\n}\n")
	return b.String()
}

func initRepo(t *testing.T) string {
	t.Helper()
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	root := t.TempDir()
	mustGit(t, root, "init", "-q", "-b", "main")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	return root
}

// TestReview_FlagsNewComplexFunction commits a clean baseline, then a second
// commit adding a high-complexity function, and asserts review (base=HEAD~1)
// returns a fail verdict with a located complexity finding.
func TestReview_FlagsNewComplexFunction(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	commitAs(t, root, "simple.go", "package m\n\nfunc Simple() int { return 1 }\n", "Dev", "dev@example.com")
	// Second commit: the complex function lands in its own file.
	commitAs(t, root, "branchy.go", branchyFunc("m"), "Dev", "dev@example.com")

	res, err := search.Review(context.Background(), search.Options{Root: root, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD~1", CheckDeadCode: false})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Verdict != "fail" {
		t.Fatalf("verdict = %q, want fail (findings: %+v)", res.Verdict, res.Findings)
	}
	if res.FailCount == 0 {
		t.Fatalf("FailCount = 0, want > 0")
	}
	var found bool
	for _, f := range res.Findings {
		if f.Rule == "complexity" && strings.HasSuffix(f.Path, "branchy.go") && f.Symbol == "Branchy" {
			found = true
			if f.Level != "fail" || f.StartLine <= 0 {
				t.Errorf("complexity finding = %+v, want fail-level with a line", f)
			}
		}
	}
	if !found {
		t.Errorf("no complexity finding for branchy.go Branchy in %+v", res.Findings)
	}
	// simple.go was in HEAD~2, not in the HEAD~1...HEAD diff — must not appear.
	for _, c := range res.ChangedFiles {
		if strings.HasSuffix(c, "simple.go") {
			t.Errorf("simple.go should not be in the diff: %v", res.ChangedFiles)
		}
	}
}

// TestReview_FlagsCognitiveComplexity: a deeply-nested function with LOW
// cyclomatic complexity (6 nested ifs → cyclomatic 7, under the gate) but HIGH
// cognitive complexity (1+2+…+6 = 21, over the gate) is flagged by the
// cognitive gate and not the cyclomatic one — proving cognitive catches what
// cyclomatic misses.
func TestReview_FlagsCognitiveComplexity(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	commitAs(t, root, "base.go", "package m\n\nfunc Base() int { return 0 }\n", "Dev", "dev@example.com")
	deep := "package m\n\nfunc Deep(x int) int {\n" +
		"\tif x > 1 {\n\t\tif x > 2 {\n\t\t\tif x > 3 {\n\t\t\t\tif x > 4 {\n\t\t\t\t\tif x > 5 {\n\t\t\t\t\t\tif x > 6 {\n\t\t\t\t\t\t\treturn x\n\t\t\t\t\t\t}\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n\treturn 0\n}\n"
	commitAs(t, root, "deep.go", deep, "Dev", "dev@example.com")

	res, err := search.Review(context.Background(), search.Options{Root: root, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD~1", CheckDeadCode: false})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Verdict != "fail" {
		t.Fatalf("verdict = %q, want fail (findings: %+v)", res.Verdict, res.Findings)
	}
	var cognitive, cyclomatic bool
	for _, f := range res.Findings {
		switch f.Rule {
		case "cognitive-complexity":
			cognitive = true
		case "complexity":
			cyclomatic = true
		}
	}
	if !cognitive {
		t.Errorf("expected a cognitive-complexity finding for Deep; got %+v", res.Findings)
	}
	if cyclomatic {
		t.Errorf("did not expect a cyclomatic complexity finding (Deep's cyclomatic is 7, under the gate): %+v", res.Findings)
	}
}

// TestReview_PassOnCleanDiff: a changed file with only a trivial function
// produces a pass verdict (no findings).
func TestReview_PassOnCleanDiff(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	commitAs(t, root, "a.go", "package m\n\nfunc A() int { return 1 }\n", "Dev", "dev@example.com")
	commitAs(t, root, "b.go", "package m\n\nfunc B() int { return A() }\n", "Dev", "dev@example.com")

	res, err := search.Review(context.Background(), search.Options{Root: root, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD~1", CheckDeadCode: false})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Verdict != "pass" {
		t.Errorf("verdict = %q, want pass (findings: %+v)", res.Verdict, res.Findings)
	}
	if len(res.Findings) != 0 {
		t.Errorf("findings = %+v, want none", res.Findings)
	}
}

// TestReview_DeadCodeWarn: an unreferenced function in a changed file is a
// warn-level finding and produces a warn verdict (no fail).
func TestReview_DeadCodeWarn(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	commitAs(t, root, "main.go", "package main\n\nfunc main() { _ = live() }\n\nfunc live() int { return 1 }\n", "Dev", "dev@example.com")
	// Second commit: a function nothing references.
	commitAs(t, root, "helper.go", "package main\n\nfunc deadHelper() int { return 42 }\n", "Dev", "dev@example.com")

	res, err := search.Review(context.Background(), search.Options{Root: root, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD~1", CheckDeadCode: true})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	var found bool
	for _, f := range res.Findings {
		if f.Rule == "dead-code" && strings.HasSuffix(f.Path, "helper.go") {
			found = true
			if f.Level != "warn" {
				t.Errorf("dead-code finding level = %q, want warn", f.Level)
			}
		}
	}
	if !found {
		t.Fatalf("no dead-code finding for helper.go in %+v", res.Findings)
	}
	if res.Verdict != "warn" {
		t.Errorf("verdict = %q, want warn", res.Verdict)
	}
	if res.FailCount != 0 {
		t.Errorf("FailCount = %d, want 0 (dead-code never fails)", res.FailCount)
	}
}

// TestReview_EmptyDiffPasses: when nothing changed (base == HEAD), the verdict
// is pass and no analysis runs.
func TestReview_EmptyDiffPasses(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	commitAs(t, root, "a.go", "package m\n\nfunc A() int { return 1 }\n", "Dev", "dev@example.com")

	res, err := search.Review(context.Background(), search.Options{Root: root, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD", CheckDeadCode: true})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Verdict != "pass" || len(res.ChangedFiles) != 0 {
		t.Errorf("verdict=%q changed=%v, want pass with no changed files", res.Verdict, res.ChangedFiles)
	}
}

// TestReview_ScopesToSubdir: reviewing a subdirectory only reports changes
// under it, not changes elsewhere in the repo (the git pathspec scoping).
func TestReview_ScopesToSubdir(t *testing.T) {
	root := initRepo(t)
	commitAs(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n", "Dev", "dev@example.com")
	// One commit touching two subtrees.
	commitAs(t, root, "sub1/a.go", "package sub1\n\nfunc A() int { return 1 }\n", "Dev", "dev@example.com")
	commitAs(t, root, "sub2/b.go", "package sub2\n\nfunc B() int { return 1 }\n", "Dev", "dev@example.com")

	res, err := search.Review(context.Background(),
		search.Options{Root: filepath.Join(root, "sub1"), Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{Base: "HEAD~2", CheckDeadCode: false})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	for _, c := range res.ChangedFiles {
		if strings.Contains(c, "sub2") {
			t.Errorf("sub2 leaked into a sub1-scoped review: %v", res.ChangedFiles)
		}
	}
	var sawSub1 bool
	for _, c := range res.ChangedFiles {
		if strings.Contains(c, "sub1") {
			sawSub1 = true
		}
	}
	if !sawSub1 {
		t.Errorf("expected sub1 changes in %v", res.ChangedFiles)
	}
	if res.FilesAnalysed != len(res.ChangedFiles) {
		t.Errorf("FilesAnalysed = %d, want %d (len changed files)", res.FilesAnalysed, len(res.ChangedFiles))
	}
}

// TestReview_NonGitDirErrors: a non-repo directory surfaces a clear error.
func TestReview_NonGitDirErrors(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir() // not a git repo
	_, err := search.Review(context.Background(), search.Options{Root: dir, Workers: 1},
		contentpkg.DefaultRegistry(), search.ReviewConfig{})
	if err == nil {
		t.Fatal("expected an error for a non-git directory")
	}
}

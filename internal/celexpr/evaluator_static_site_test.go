package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/projecttype"
)

// TestBuildAttributesWith_StaticSitePredicateHugo verifies that a file
// inside a Hugo project surfaces is_static_site=true via attrs.Extra
// AND that the CEL evaluator returns true for the predicate. Covers
// the four-place invariant end-to-end: builtin registration → Extra
// population → CEL variable declaration → activation lookup.
func TestBuildAttributesWith_StaticSitePredicateHugo(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hugo.toml"), []byte("baseURL = '/'\n"), 0o644); err != nil {
		t.Fatalf("write hugo.toml: %v", err)
	}
	postPath := filepath.Join(root, "post.md")
	if err := os.WriteFile(postPath, []byte("# title\nhello\n"), 0o644); err != nil {
		t.Fatalf("write post.md: %v", err)
	}

	abs, err := filepath.Abs(postPath)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	opts := celexpr.BuildOptions{
		ProjectResolver: projecttype.NewResolver(root, nil),
	}
	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}

	// 1. Extra carries the explicit flag.
	flag, ok := attrs.Extra["is_static_site"].(bool)
	if !ok || !flag {
		t.Errorf("attrs.Extra[is_static_site] = %v / ok=%v; want true / true", attrs.Extra["is_static_site"], ok)
	}
	// 2. project_type populates to "hugo".
	if pt, _ := attrs.Extra["project_type"].(string); pt != "hugo" {
		t.Errorf("project_type=%q want \"hugo\"", pt)
	}

	// 3. The CEL evaluator returns true for is_static_site.
	ev, err := celexpr.New("is_static_site")
	if err != nil {
		t.Fatalf("celexpr.New: %v", err)
	}
	match, err := ev.Evaluate(attrs)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !match {
		t.Errorf("is_static_site CEL expr returned false; want true")
	}

	// 4. Confirms the predicate composes with project_type for
	// hugo-only queries.
	ev2, err := celexpr.New("is_static_site && project_type == \"hugo\"")
	if err != nil {
		t.Fatalf("celexpr.New (combined): %v", err)
	}
	combined, err := ev2.Evaluate(attrs)
	if err != nil {
		t.Fatalf("Evaluate combined: %v", err)
	}
	if !combined {
		t.Errorf("is_static_site && project_type == hugo returned false; want true")
	}
}

// TestBuildAttributesWith_StaticSitePredicateNonSSG verifies that a
// file inside a non-SSG project (here, a Go module) leaves
// is_static_site at its zero default (false).
func TestBuildAttributesWith_StaticSitePredicateNonSSG(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	abs, err := filepath.Abs(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	opts := celexpr.BuildOptions{
		ProjectResolver: projecttype.NewResolver(root, nil),
	}
	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}

	// Extra should NOT carry the flag (Go isn't an SSG).
	if v, ok := attrs.Extra["is_static_site"]; ok {
		t.Errorf("attrs.Extra[is_static_site] = %v; want absent (Go isn't an SSG)", v)
	}

	// CEL should evaluate to false.
	ev, err := celexpr.New("is_static_site")
	if err != nil {
		t.Fatalf("celexpr.New: %v", err)
	}
	match, err := ev.Evaluate(attrs)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if match {
		t.Errorf("is_static_site returned true for a Go module; want false")
	}
}

// TestBuildAttributesWith_StaticSitePredicateNoResolver verifies the
// predicate is silently false when ResolveProjects isn't opted in —
// matches the project_type / project_types contract.
func TestBuildAttributesWith_StaticSitePredicateNoResolver(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hugo.toml"), []byte("baseURL = '/'\n"), 0o644); err != nil {
		t.Fatalf("write hugo.toml: %v", err)
	}
	postPath := filepath.Join(root, "post.md")
	if err := os.WriteFile(postPath, []byte("# title\n"), 0o644); err != nil {
		t.Fatalf("write post.md: %v", err)
	}

	abs, err := filepath.Abs(postPath)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Note: no ProjectResolver in opts.
	opts := celexpr.BuildOptions{}
	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if v, ok := attrs.Extra["is_static_site"]; ok {
		t.Errorf("attrs.Extra[is_static_site] = %v; want absent without ResolveProjects", v)
	}

	ev, err := celexpr.New("is_static_site")
	if err != nil {
		t.Fatalf("celexpr.New: %v", err)
	}
	match, err := ev.Evaluate(attrs)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if match {
		t.Errorf("is_static_site = true without ResolveProjects; want false")
	}
}

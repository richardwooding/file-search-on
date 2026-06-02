package content

import (
	"context"
	"testing"
	"testing/fstest"
)

// TestIsSourceTestFile exercises the per-language basename matcher
// against representative test-file and non-test-file paths.
func TestIsSourceTestFile(t *testing.T) {
	cases := []struct {
		language string
		path     string
		want     bool
	}{
		// Go.
		{"go", "foo_test.go", true},
		{"go", "internal/pkg/bar_test.go", true},
		{"go", "foo.go", false},
		{"go", "test_foo.go", false}, // not the Go convention
		// Python.
		{"python", "test_foo.py", true},
		{"python", "foo_test.py", true},
		{"python", "tests/test_bar.py", true},
		{"python", "foo.py", false},
		{"python", "testify.py", false}, // not prefix test_
		// JavaScript / TypeScript.
		{"javascript", "foo.test.js", true},
		{"javascript", "foo.spec.jsx", true},
		{"typescript", "foo.test.ts", true},
		{"typescript", "foo.test.tsx", true},
		{"typescript", "foo.spec.ts", true},
		{"typescript", "foo.ts", false},
		// Rust.
		{"rust", "tests/integration.rs", true},
		{"rust", "src/lib.rs", false},
		{"rust", "src/tests.rs", false}, // module name, not the integration-test convention
		// C / C++.
		{"c", "test_widget.c", true},
		{"c", "widget_test.c", true},
		{"c", "widget_tests.c", true},
		{"c", "widget.c", false},
		{"cpp", "test_foo.cpp", true},
		{"cpp", "foo_test.cpp", true},
		{"cpp", "foo.cpp", false},
		// Java.
		{"java", "WidgetTest.java", true},
		{"java", "WidgetTests.java", true},
		{"java", "WidgetIT.java", true},
		{"java", "Widget.java", false},
		// Ruby.
		{"ruby", "widget_spec.rb", true},
		{"ruby", "widget_test.rb", true},
		{"ruby", "widget.rb", false},
		// Swift.
		{"swift", "WidgetTests.swift", true},
		{"swift", "Widget.swift", false},
		// Kotlin.
		{"kotlin", "WidgetTest.kt", true},
		{"kotlin", "Widget.kt", false},
		// Scala.
		{"scala", "WidgetSpec.scala", true},
		{"scala", "WidgetTest.scala", true},
		{"scala", "Widget.scala", false},
		// Shell.
		{"shell", "foo_test.sh", true},
		{"shell", "test_foo.sh", true},
		{"shell", "foo.sh", false},
		// Elixir.
		{"elixir", "widget_test.exs", true},
		{"elixir", "widget.ex", false},
		// Tiobe top 20 (May 2026) additions.
		// C# — xUnit / NUnit / MSTest.
		{"csharp", "WidgetTest.cs", true},
		{"csharp", "WidgetTests.cs", true},
		{"csharp", "Widget.cs", false},
		// PHP — PHPUnit.
		{"php", "WidgetTest.php", true},
		{"php", "Widget.php", false},
		// Perl — .t is the test extension.
		{"perl", "01-basic.t", true},
		{"perl", "module.pm", false},
		{"perl", "script.pl", false},
		// R — testthat.
		{"r", "test-foo.R", true},
		{"r", "test_bar.r", true},
		{"r", "analysis.R", false},
		// Visual Basic.
		{"vb", "WidgetTest.vb", true},
		{"vb", "WidgetTests.vb", true},
		{"vb", "Widget.vb", false},
		// MATLAB.
		{"matlab", "WidgetTest.m", true},
		{"matlab", "WidgetTests.m", true},
		{"matlab", "widget.m", false},
		// Languages without a strong convention — never test.
		{"lua", "widget.lua", false},
		{"haskell", "Widget.hs", false},
		{"ada", "widget.adb", false},
		{"sql", "schema.sql", false},
		{"fortran", "solver.f90", false},
		{"assembly", "boot.asm", false},
		{"pascal", "main.pas", false},
	}
	for _, c := range cases {
		t.Run(c.language+"/"+c.path, func(t *testing.T) {
			got := isSourceTestFile(c.language, c.path)
			if got != c.want {
				t.Errorf("isSourceTestFile(%q, %q) = %v, want %v",
					c.language, c.path, got, c.want)
			}
		})
	}
}

// TestSourceType_TestFileAttribute confirms the source/* parser
// surfaces `is_test_file` in its Attributes map when the basename
// matches. Spot-check on Go since other languages are unit-covered
// via TestIsSourceTestFile.
func TestSourceType_TestFileAttribute(t *testing.T) {
	body := []byte("package foo\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	fsys := fstest.MapFS{
		"foo_test.go": &fstest.MapFile{Data: body},
		"foo.go":      &fstest.MapFile{Data: []byte("package foo\n\nfunc X() {}\n")},
	}
	cases := []struct {
		path        string
		wantTest    bool
		wantLang    string
	}{
		{"foo_test.go", true, "go"},
		{"foo.go", false, "go"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			ct := DefaultRegistry().Detect(fsys, c.path)
			if ct == nil || ct.Name() != "source/go" {
				t.Fatalf("Detect: got %v, want source/go", ct)
			}
			attrs, err := ct.Attributes(context.Background(), fsys, c.path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got, _ := attrs["is_test_file"].(bool); got != c.wantTest {
				t.Errorf("is_test_file = %v, want %v", got, c.wantTest)
			}
			if got := attrs["language"]; got != c.wantLang {
				t.Errorf("language = %v, want %q", got, c.wantLang)
			}
		})
	}
}

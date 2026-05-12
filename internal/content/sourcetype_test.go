package content_test

import (
	"testing"
	"testing/fstest"

	"github.com/richardwooding/file-search-on/internal/content"
)

// TestSourceCounting drives the line classifier across a curated set
// of inputs covering every interesting branch: line comments only,
// block comments, mixed, only-blank, only-code, mid-block blank.
func TestSourceCounting(t *testing.T) {
	cases := []struct {
		name                                   string
		path                                   string
		body                                   string
		wantLang                               string
		wantTotal, wantLOC, wantComm, wantBlnk int64
	}{
		{
			name:      "go: line+block+blank",
			path:      "a.go",
			body:      "// hdr 1\n// hdr 2\npackage main\n\n/*\nblock\n*/\n\nfunc main() {}\n",
			wantLang:  "go",
			wantTotal: 9, wantLOC: 2, wantComm: 5, wantBlnk: 2,
		},
		{
			name:      "python: hash-only comments",
			path:      "a.py",
			body:      "# top\n\ndef f():\n    # inline\n    return 1\n",
			wantLang:  "python",
			wantTotal: 5, wantLOC: 2, wantComm: 2, wantBlnk: 1,
		},
		{
			name:      "ocaml: block-only (no line comment)",
			path:      "a.ml",
			body:      "(* docstring *)\nlet x = 1\n",
			wantLang:  "ocaml",
			wantTotal: 2, wantLOC: 1, wantComm: 1, wantBlnk: 0,
		},
		{
			name:      "lua: -- line + --[[ block ]]",
			path:      "a.lua",
			body:      "-- top\n--[[\nblock\n]]\nlocal x = 1\n",
			wantLang:  "lua",
			wantTotal: 5, wantLOC: 1, wantComm: 4, wantBlnk: 0,
		},
		{
			name:      "clojure: ;-line, no block",
			path:      "a.clj",
			body:      "; comment\n(println \"hi\")\n",
			wantLang:  "clojure",
			wantTotal: 2, wantLOC: 1, wantComm: 1, wantBlnk: 0,
		},
		{
			name:      "rust: /// doc comments classify as comment",
			path:      "a.rs",
			body:      "/// docstring\nfn main() {}\n",
			wantLang:  "rust",
			wantTotal: 2, wantLOC: 1, wantComm: 1, wantBlnk: 0,
		},
		{
			name:      "single-line block comment opens and closes on one line",
			path:      "a.go",
			body:      "/* one-liner */\npackage main\n",
			wantLang:  "go",
			wantTotal: 2, wantLOC: 1, wantComm: 1, wantBlnk: 0,
		},
		{
			name:      "blank inside block counts as blank (cloc convention)",
			path:      "a.go",
			body:      "/*\n\nstill in block\n*/\n",
			wantLang:  "go",
			wantTotal: 4, wantLOC: 0, wantComm: 3, wantBlnk: 1,
		},
		{
			name:      "code with trailing comment counts as code",
			path:      "a.go",
			body:      "x := 1 // trailing\n",
			wantLang:  "go",
			wantTotal: 1, wantLOC: 1, wantComm: 0, wantBlnk: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{c.path: {Data: []byte(c.body)}}
			ct := content.DefaultRegistry().Detect(fsys, c.path)
			if ct == nil {
				t.Fatalf("Detect returned nil for %q", c.path)
			}
			a, err := ct.Attributes(t.Context(), fsys, c.path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if a["language"] != c.wantLang {
				t.Errorf("language = %q; want %q", a["language"], c.wantLang)
			}
			if v, _ := a["line_count"].(int64); v != c.wantTotal {
				t.Errorf("line_count = %v; want %d", a["line_count"], c.wantTotal)
			}
			if v, _ := a["loc"].(int64); v != c.wantLOC {
				t.Errorf("loc = %v; want %d", a["loc"], c.wantLOC)
			}
			if v, _ := a["comment_loc"].(int64); v != c.wantComm {
				t.Errorf("comment_loc = %v; want %d", a["comment_loc"], c.wantComm)
			}
			if v, _ := a["blank_loc"].(int64); v != c.wantBlnk {
				t.Errorf("blank_loc = %v; want %d", a["blank_loc"], c.wantBlnk)
			}
		})
	}
}

// TestSourceDetection_ByExtension verifies a handful of less-common
// extensions route to the right language.
func TestSourceDetection_ByExtension(t *testing.T) {
	cases := []struct {
		path     string
		wantName string
	}{
		{"a.go", "source/go"},
		{"a.py", "source/python"},
		{"a.pyi", "source/python"},
		{"a.tsx", "source/typescript"},
		{"a.mjs", "source/javascript"},
		{"a.rs", "source/rust"},
		{"a.hpp", "source/cpp"},
		{"a.h", "source/c"},
		{"a.kt", "source/kotlin"},
		{"a.ex", "source/elixir"},
		{"a.exs", "source/elixir"},
		{"a.cljs", "source/clojure"},
		{"a.zig", "source/zig"},
		{"a.scala", "source/scala"},
		{"a.sc", "source/scala"},
	}
	for _, c := range cases {
		fsys := fstest.MapFS{c.path: {Data: []byte("")}}
		ct := content.DefaultRegistry().Detect(fsys, c.path)
		if ct == nil {
			t.Errorf("Detect(%q) = nil; want %s", c.path, c.wantName)
			continue
		}
		if ct.Name() != c.wantName {
			t.Errorf("Detect(%q).Name() = %q; want %s", c.path, ct.Name(), c.wantName)
		}
	}
}

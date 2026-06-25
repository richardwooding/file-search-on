package content

import "testing"

// TestMigratedLanguages checks tree-sitter extraction parity for the 8
// languages migrated off regex (#365): functions, type_names, imports,
// and at least one reference. Membership checks (extractor may find more).
func TestMigratedLanguages(t *testing.T) {
	cases := []struct {
		language    string
		src         string
		wantFuncs   []string
		wantTypes   []string
		wantImports []string
		wantRefs    []string
	}{
		{
			language:    "python",
			src:         "import os\nfrom sys import path\n\nclass Widget:\n    def greet(self):\n        return helper()\n\ndef top():\n    pass\n",
			wantFuncs:   []string{"greet", "top"},
			wantTypes:   []string{"Widget"},
			wantImports: []string{"os", "sys"},
			wantRefs:    []string{"helper"},
		},
		{
			language:    "java",
			src:         "package p;\nimport java.util.List;\n\nclass Widget {\n  void greet() { helper(); }\n}\ninterface Greeter {}\nenum Color { RED }\n",
			wantFuncs:   []string{"greet"},
			wantTypes:   []string{"Widget", "Greeter", "Color"},
			wantImports: []string{"java.util.List"},
			wantRefs:    []string{"helper"},
		},
		{
			language:    "csharp",
			src:         "using System;\nusing System.Collections.Generic;\n\nclass Widget {\n  void Greet() { Helper(); }\n}\nstruct Point {}\ninterface IGreeter {}\nenum Color { Red }\n",
			wantFuncs:   []string{"Greet"},
			wantTypes:   []string{"Widget", "Point", "IGreeter", "Color"},
			wantImports: []string{"System", "System.Collections.Generic"},
			wantRefs:    []string{"Helper"},
		},
		{
			language:    "php",
			src:         "<?php\nnamespace App;\nuse Foo\\Bar;\n\nclass Widget {\n  function greet() { helper(); }\n}\ninterface Greeter {}\ntrait T {}\nfunction topLevel() {}\n",
			wantFuncs:   []string{"greet", "topLevel"},
			wantTypes:   []string{"Widget", "Greeter", "T"},
			wantImports: []string{"Foo\\Bar"},
			wantRefs:    []string{"helper"},
		},
		{
			language: "perl",
			// `$self->process()` is a method_call_expression — captured as a
			// reference since the refExtractionLangs expansion (#444). Without
			// it, Perl method calls were invisible and dead_code over-reported
			// (mojo dead ratio 84% -> 23% once method calls counted).
			src:         "use strict;\nuse List::Util;\npackage Widget;\n\nsub greet { my $self = shift; helper(); $self->process(); }\nsub other { }\n",
			wantFuncs:   []string{"greet", "other"},
			wantTypes:   []string{"Widget"},
			wantImports: []string{"strict", "List::Util"},
			wantRefs:    []string{"helper", "process"},
		},
		{
			language:    "r",
			src:         "library(ggplot2)\nrequire(dplyr)\n\ngreet <- function(x) { helper(x) }\nother <- function() {}\n",
			wantFuncs:   []string{"greet", "other"},
			wantImports: []string{"ggplot2", "dplyr"},
			wantRefs:    []string{"helper"},
		},
		{
			language:    "matlab",
			src:         "function greet(x)\n  helper(x);\nend\nfunction other()\nend\n",
			wantFuncs:   []string{"greet", "other"},
			wantRefs:    []string{"helper"},
		},
		{
			language:    "scala",
			src:         "package p\nimport scala.collection.mutable.ListBuffer\n\nclass Widget {\n  def greet(): Unit = helper()\n}\nobject Reg\ntrait Greeter\n",
			wantFuncs:   []string{"greet"},
			wantTypes:   []string{"Widget", "Reg", "Greeter"},
			wantImports: []string{"scala.collection.mutable.ListBuffer"},
			wantRefs:    []string{"helper"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			funcs, types, imports, refs, _, _, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "functions", funcs, tc.wantFuncs)
			checkContains(t, "type_names", types, tc.wantTypes)
			checkContains(t, "imports", imports, tc.wantImports)
			checkContains(t, "references", refs, tc.wantRefs)
		})
	}
}

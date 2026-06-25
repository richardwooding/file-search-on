package content

import (
	"sort"
	"strings"
	"testing"
)

// This file is the cross-language symbol-extraction fidelity benchmark
// (issue #444). Its job is to FAIL CI the moment a supported source
// language silently regresses to zero on the code-intelligence signals
// agents depend on — functions, references (call sites), and cyclomatic
// complexity. Several real regressions shipped before this guard existed:
// C# complexity was silently 0 for every file (#439, a typo'd decision
// node), and JS dropped all CommonJS imports.
//
// Two guards:
//  1. langBaselines asserts each wired language extracts at least the
//     expected functions / references / complexity from a representative
//     snippet.
//  2. TestExtractionCoverage_AllWiredLanguagesBenchmarked enumerates every
//     registered source/* language and fails if a language with a symbol
//     extractor (symbolExtractorWired) has no baseline here — so adding a
//     language forces a fidelity fixture, and dropping an extractor is
//     caught too.

// extractSymbolsFor dispatches to the same extractor sourcetype.go uses:
// the stdlib-AST path for Go, tree-sitter for everything else.
func extractSymbolsFor(language string, src []byte) (funcs, types, imports, refs, callEdges, complexityRows []string) {
	if language == "go" {
		funcs, types, imports, refs, callEdges, complexityRows, _ = extractGoSymbols(src)
		return
	}
	funcs, types, imports, refs, callEdges, complexityRows, _ = extractTreeSitterSymbols(language, src)
	return
}

type langBaseline struct {
	src string
	// Minimum counts the extractor must meet. These encode the CURRENT
	// working baseline, not aspirations — raise them as fidelity improves;
	// a drop below them is a regression.
	minFuncs      int
	minRefs       int   // call-site references; 0 = not asserted (see notes)
	minComplexity int64 // max per-function complexity; 0 = not asserted
	note          string
}

// Each snippet defines two functions (a calls helper), a type, an import,
// and a branch inside `a` (so complexity is 1 + 1 = 2). Snippets are kept
// to constructs the grammar parses cleanly today.
var langBaselines = map[string]langBaseline{
	"go":         {src: "package p\nimport \"fmt\"\ntype T struct{}\nfunc a(x int) int { if x > 0 { helper() }; fmt.Println(); return x }\nfunc helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"python":     {src: "import os\nclass T: pass\ndef a(x):\n    if x > 0:\n        helper()\n    return x\ndef helper(): pass\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"java":       {src: "import java.util.List;\nclass T {}\nclass C { int a(int x){ if(x>0){helper();} return x; } void helper(){} }\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"csharp":     {src: "using System;\nclass T {}\nclass C { int a(int x){ if(x>0){helper();} return x; } void helper(){} }\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"php":        {src: "<?php\nuse Foo\\Bar;\nclass T {}\nfunction a($x){ if($x>0){ helper(); } return $x; }\nfunction helper(){}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"perl":       {src: "use strict;\npackage T;\nsub a { my $x = shift; if ($x > 0) { helper(); } return $x; }\nsub helper {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"r":          {src: "library(dplyr)\na <- function(x){ if (x > 0) { helper() }; x }\nhelper <- function(){}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"matlab":     {src: "function y = a(x)\n  if x > 0\n    helper();\n  end\n  y = x;\nend\nfunction helper()\nend\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"scala":      {src: "import scala.collection.mutable.ListBuffer\nclass T\nobject C { def a(x: Int): Int = { if (x > 0) { helper() }; x }; def helper(): Unit = {} }\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"rust":       {src: "use std::fmt;\nstruct T;\nfn a(x: i32) -> i32 { if x > 0 { helper(); } x }\nfn helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"typescript": {src: "import { X } from \"./x\";\ninterface T { n: number }\nfunction a(x: number): number { if (x > 0) { helper(); } return x; }\nfunction helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"javascript": {src: "const x = require(\"./x\");\nclass T {}\nfunction a(x){ if (x > 0) { helper(); } return x; }\nfunction helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	// Ruby: block `if` (not the postfix modifier form, which the decision
	// query doesn't count) so complexity exercises a decision point.
	"ruby": {src: "require \"json\"\nclass T\n  def a(x)\n    if x > 0\n      helper\n    end\n    x\n  end\n  def helper; end\nend\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	// Swift: comparison operators in an if/while condition of a
	// parameterized function fail to parse (#449), so the branch uses a
	// bool parameter — which the grammar handles — to exercise complexity.
	"swift":  {src: "import Foundation\nclass T {}\nfunc a(flag: Bool) -> Int { if flag { helper() }; return 0 }\nfunc helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2, note: "comparison-in-condition gap tracked in #449"},
	"kotlin": {src: "import kotlin.math.abs\nclass T\nfun a(x: Int): Int { if (x > 0) { helper() }; return x }\nfun helper() {}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"c":      {src: "#include <stdio.h>\nstruct T {};\nint a(int x){ if(x>0){ helper(); } return x; }\nvoid helper(){}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
	"cpp":    {src: "#include <vector>\nstruct T {};\nint a(int x){ if(x>0){ helper(); } return x; }\nvoid helper(){}\n", minFuncs: 2, minRefs: 1, minComplexity: 2},
}

// detectionOnlySourceLangs are registered source/* languages that are
// detected (content_type + is_source) but have NO symbol extractor wired
// (symbolExtractorWired is false). Listed explicitly so the completeness
// guard can tell "intentionally extractor-less" from "forgot a baseline".
var detectionOnlySourceLangs = map[string]bool{
	"ada": true, "assembly": true, "clojure": true, "elixir": true,
	"fortran": true, "haskell": true, "lua": true, "ocaml": true,
	"pascal": true, "shell": true, "sql": true, "vb": true, "zig": true,
}

func TestExtractionCoverage_Baselines(t *testing.T) {
	langs := make([]string, 0, len(langBaselines))
	for l := range langBaselines {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		b := langBaselines[lang]
		t.Run(lang, func(t *testing.T) {
			funcs, _, _, refs, _, cplx := extractSymbolsFor(lang, []byte(b.src))
			if len(funcs) < b.minFuncs {
				t.Errorf("functions: got %d %v, want >= %d", len(funcs), funcs, b.minFuncs)
			}
			if b.minRefs > 0 && len(refs) < b.minRefs {
				t.Errorf("references: got %d %v, want >= %d", len(refs), refs, b.minRefs)
			}
			if b.minComplexity > 0 {
				if mc := maxComplexityOf(cplx); mc < b.minComplexity {
					t.Errorf("max complexity: got %d, want >= %d (rows=%v)", mc, b.minComplexity, cplx)
				}
			}
		})
	}
}

// TestExtractionCoverage_AllWiredLanguagesBenchmarked enforces that the
// benchmark above tracks every language with a symbol extractor, and that
// every registered source/* language is accounted for (wired+benchmarked
// or explicitly detection-only). Adding a source language without a
// fidelity baseline — or removing an extractor — fails here.
func TestExtractionCoverage_AllWiredLanguagesBenchmarked(t *testing.T) {
	for _, ct := range DefaultRegistry().Types() {
		lang, ok := strings.CutPrefix(ct.Name(), "source/")
		if !ok {
			continue
		}
		wired := symbolExtractorWired(lang)
		_, benched := langBaselines[lang]
		_, detectionOnly := detectionOnlySourceLangs[lang]
		switch {
		case wired && !benched:
			t.Errorf("source/%s has a symbol extractor but no fidelity baseline in langBaselines — add one (see #444)", lang)
		case !wired && !detectionOnly:
			t.Errorf("source/%s has no symbol extractor and isn't in detectionOnlySourceLangs — classify it", lang)
		case wired && detectionOnly:
			t.Errorf("source/%s is both wired and listed detection-only — remove it from detectionOnlySourceLangs", lang)
		}
	}
}

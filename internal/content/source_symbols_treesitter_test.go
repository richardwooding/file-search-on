package content

import (
	"slices"
	"testing"
)

// tsCase is one language fixture with the symbols we expect the
// tree-sitter extractor to surface. Lists are membership checks (the
// extractor may legitimately find more), except imports which we check
// by membership too.
type tsCase struct {
	language    string
	src         string
	wantFuncs   []string
	wantTypes   []string
	wantImports []string
}

func TestExtractTreeSitterSymbols(t *testing.T) {
	cases := []tsCase{
		{
			language: "rust",
			src: `use std::collections::HashMap;
use serde::Serialize;

pub struct Widget { name: String }
pub trait Greeter { fn greet(&self) -> String; }
pub fn build() -> Widget { Widget { name: String::new() } }
`,
			wantFuncs:   []string{"build", "greet"},
			wantTypes:   []string{"Widget", "Greeter"},
			wantImports: []string{"std::collections::HashMap", "serde::Serialize"},
		},
		{
			language: "typescript",
			src: `import { Foo } from "./foo";
import * as path from "path";

export class Service {
  handle(): void {}
}
export function run(): number { return 1; }
interface Opts { x: number }
`,
			wantFuncs:   []string{"handle", "run"},
			wantTypes:   []string{"Service", "Opts"},
			wantImports: []string{"./foo", "path"},
		},
		{
			language: "javascript",
			src: `import { Foo } from "./foo";

export class Service {
  handle() {}
}
export function run() { return 1; }
`,
			wantFuncs:   []string{"handle", "run"},
			wantTypes:   []string{"Service"},
			wantImports: []string{"./foo"},
		},
		{
			language: "ruby",
			src: `require "json"
require_relative "helper"

class Widget
  def greet
    "hi"
  end
end

module Greetable
end
`,
			wantFuncs:   []string{"greet"},
			wantTypes:   []string{"Widget", "Greetable"},
			wantImports: []string{"json", "helper"},
		},
		{
			language: "swift",
			src: `import Foundation
import UIKit

class Widget {
  func greet() -> String { return "hi" }
}
struct Point { var x: Int }
protocol Greeter { func greet() -> String }
`,
			wantFuncs:   []string{"greet"},
			wantTypes:   []string{"Widget", "Point", "Greeter"},
			wantImports: []string{"Foundation", "UIKit"},
		},
		{
			language: "kotlin",
			src: `import kotlin.collections.List
import java.util.Date

object Registry

class Widget {
  fun greet(): String = "hi"
}
`,
			wantFuncs:   []string{"greet"},
			wantTypes:   []string{"Widget", "Registry"},
			wantImports: []string{"kotlin.collections.List", "java.util.Date"},
		},
		{
			language: "c",
			src: `#include <stdio.h>
#include "local.h"

struct Point { int x; };
int add(int a, int b) { return a + b; }
`,
			wantFuncs:   []string{"add"},
			wantTypes:   []string{"Point"},
			wantImports: []string{"stdio.h", "local.h"},
		},
		{
			language: "cpp",
			src: `#include <vector>
#include "widget.h"

class Widget {
public:
  void greet();
};
int main() { return 0; }
`,
			wantFuncs:   []string{"main"},
			wantTypes:   []string{"Widget"},
			wantImports: []string{"vector", "widget.h"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			funcs, types, imports, _, _, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "functions", funcs, tc.wantFuncs)
			checkContains(t, "type_names", types, tc.wantTypes)
			checkContains(t, "imports", imports, tc.wantImports)
		})
	}
}

func checkContains(t *testing.T, label string, got, want []string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("%s missing %q; got %v", label, w, got)
		}
	}
}

// TestExtractTreeSitterReferences checks call-site (callee name) capture
// per language — the @reference half powering who_calls / dead_code.
func TestExtractTreeSitterReferences(t *testing.T) {
	cases := []struct {
		language string
		src      string
		wantRefs []string
	}{
		{"rust", `fn main() { setup(); thing.run(); helper::go(); println!("x"); }`, []string{"setup", "run", "go", "println"}},
		{"typescript", `function main() { setup(); obj.run(); }`, []string{"setup", "run"}},
		{"javascript", `function main() { setup(); obj.run(); }`, []string{"setup", "run"}},
		{"ruby", "def main\n  setup\n  do_thing(1)\nend\n", []string{"do_thing"}},
		{"swift", `func main() { setup(); obj.run() }`, []string{"setup", "run"}},
		{"kotlin", `fun main() { setup(); obj.run() }`, []string{"setup", "run"}},
		{"c", `int main() { setup(); compute(1); return 0; }`, []string{"setup", "compute"}},
		{"cpp", `int main() { setup(); obj.run(); return 0; }`, []string{"setup", "run"}},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			_, _, _, refs, _, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "references", refs, tc.wantRefs)
		})
	}
}

// TestExtractTreeSitterTypeRefs checks type-usage capture per language
// (#398): a type named only in a type position (field / parameter / return
// / variable / generic) must appear in references so dead_code stops
// flagging it and who_calls finds its users. Crucially the type's own
// definition name must NOT self-capture (which would defeat type dead_code),
// so each fixture names a type that is ONLY used, never defined here.
func TestExtractTreeSitterTypeRefs(t *testing.T) {
	cases := []struct {
		language string
		src      string
		wantRefs []string // type names that must surface as references
	}{
		{"rust", `struct Holder { w: Widget, items: Vec<Gadget> }
fn build(c: Cog) -> Sprocket { let b: Bolt = make(); b }`,
			[]string{"Widget", "Gadget", "Cog", "Sprocket", "Bolt"}},
		{"typescript", `class Holder { w: Widget; }
function build(c: Cog): Sprocket { let b: Bolt; return b; }`,
			[]string{"Widget", "Cog", "Sprocket", "Bolt"}},
		{"python", "def build(c: Cog) -> Sprocket:\n    b: Bolt = make()\n    return b\n",
			[]string{"Cog", "Sprocket", "Bolt"}},
		{"java", `class Holder { Widget w; Sprocket build(Cog c) { Bolt b; return b; } }`,
			[]string{"Widget", "Sprocket", "Cog", "Bolt"}},
		{"csharp", `class Holder { Widget w; void build() { Bolt b; } }`,
			[]string{"Widget", "Bolt"}}, // C# captures field + local types only
		{"c", `struct Holder { struct Widget *w; };
int build(struct Cog c) { struct Bolt b; return 0; }`,
			[]string{"Widget", "Cog", "Bolt"}},
		{"cpp", `class Holder { Widget w; };
Sprocket build(Cog c) { Bolt b; return b; }`,
			[]string{"Widget", "Sprocket", "Cog", "Bolt"}},
		{"kotlin", `class Holder(val w: Widget)
fun build(c: Cog): Sprocket { val b: Bolt = make() }`,
			[]string{"Widget", "Cog", "Sprocket", "Bolt"}},
		{"swift", `class Holder { var w: Widget }
func build(c: Cog) -> Sprocket { let b: Bolt = make() }`,
			[]string{"Widget", "Cog", "Sprocket", "Bolt"}},
		{"scala", `class Holder(w: Widget) { def build(c: Cog): Sprocket = { val b: Bolt = q } }`,
			[]string{"Widget", "Cog", "Sprocket", "Bolt"}},
		{"php", `<?php class Holder { public Widget $w; function build(Cog $c): Sprocket {} }`,
			[]string{"Widget", "Cog", "Sprocket"}},
		// JavaScript: no annotations, but a class used only via `new Foo()`
		// must surface as a reference (#444) or it reads as dead.
		{"javascript", "class Holder {}\nfunction build(){ return new Widget(); }\n",
			[]string{"Widget"}},
		// Ruby: superclass + constant receiver are the "type usages".
		{"ruby", "class Widget < Base\n  def m; Helper.go; end\nend\n",
			[]string{"Base", "Helper"}},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			_, _, _, refs, _, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "references (type usages)", refs, tc.wantRefs)
		})
	}
}

// TestExtractTreeSitterTypeRefs_NoSelfCapture pins the critical invariant:
// a type's own definition name must NOT be emitted as a reference (else it
// would self-reference and never be flagged as dead). Rust struct defined
// and never used → must not appear in references.
func TestExtractTreeSitterTypeRefs_NoSelfCapture(t *testing.T) {
	_, types, _, refs, _, _ := extractTreeSitterSymbols("rust", []byte(`struct Lonely { x: i32 }`))
	if !slices.Contains(types, "Lonely") {
		t.Fatalf("Lonely should be a defined type; got %v", types)
	}
	if slices.Contains(refs, "Lonely") {
		t.Errorf("a type's own definition must not self-capture as a reference: %v", refs)
	}
}

// TestTSExportedSymbols pins keyword-visibility extraction (#409 Phase B):
// only PUBLIC defs (Rust `pub`, TS/JS `export`) are returned, private ones
// excluded — the `exported_symbols` signal unused_exports consumes.
func TestTSExportedSymbols(t *testing.T) {
	cases := []struct {
		language    string
		src         string
		wantExp     []string
		wantNotExp  []string
	}{
		{"rust", "pub fn pf() {}\nfn priv_fn() {}\npub struct PS {}\nstruct Priv {}\npub trait PT {}",
			[]string{"pf", "PS", "PT"}, []string{"priv_fn", "Priv"}},
		{"typescript", "export function ef() {}\nfunction pf() {}\nexport class EC {}\nclass PC {}\nexport interface EI {}",
			[]string{"ef", "EC", "EI"}, []string{"pf", "PC"}},
		{"javascript", "export function ef() {}\nfunction pf() {}\nexport class EC {}\nclass PC {}",
			[]string{"ef", "EC"}, []string{"pf", "PC"}},
		{"java", "public class PubC { public int pm(){return 1;} int pkg(){return 2;} }\nclass DefC {}\npublic interface PubI {}",
			[]string{"PubC", "pm", "PubI"}, []string{"DefC", "pkg"}},
		{"csharp", "public class PubC { public void PM(){} void Pr(){} }\nclass DefC {}\npublic struct PubS {}",
			[]string{"PubC", "PM", "PubS"}, []string{"DefC", "Pr"}},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			got := tsExportedSymbols(tc.language, []byte(tc.src))
			for _, w := range tc.wantExp {
				if !slices.Contains(got, w) {
					t.Errorf("exported_symbols missing public %q: %v", w, got)
				}
			}
			for _, n := range tc.wantNotExp {
				if slices.Contains(got, n) {
					t.Errorf("private %q must not be exported: %v", n, got)
				}
			}
		})
	}
}

// TestTSNonExportedSymbols pins the default-public negation path (#409
// Phase C-2): Kotlin / Scala visibility is public unless private / internal /
// protected, so the extractor captures the NON-public names (an explicit
// Kotlin `public` is still exported and must NOT be captured).
func TestTSNonExportedSymbols(t *testing.T) {
	cases := []struct {
		language       string
		src            string
		wantNonExp     []string
		wantNotNonExp  []string // public — must not be captured
	}{
		{"kotlin", "fun pub() {}\nprivate fun priv() {}\ninternal fun intl() {}\npublic fun epub() {}\nclass PubC\nprivate class PrivC",
			[]string{"priv", "intl", "PrivC"}, []string{"pub", "epub", "PubC"}},
		{"scala", "def pub = 1\nprivate def priv = 2\nprotected def prot = 3\nclass PubC\nprivate class PrivC",
			[]string{"priv", "prot", "PrivC"}, []string{"pub", "PubC"}},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			got := tsNonExportedSymbols(tc.language, []byte(tc.src))
			for _, w := range tc.wantNonExp {
				if !slices.Contains(got, w) {
					t.Errorf("non-exported missing %q: %v", w, got)
				}
			}
			for _, n := range tc.wantNotNonExp {
				if slices.Contains(got, n) {
					t.Errorf("public %q must not be captured as non-exported: %v", n, got)
				}
			}
		})
	}
}

// TestParseTimeoutBudget guards the #432 fix: the tree-sitter parse budget
// must stay a positive, sane value. Zero would mean "no timeout" in
// gotreesitter, re-opening the door to a pathological grammar parse (Swift)
// hanging the whole walk. (The behavioural proof — AFError.swift going from
// 3+ min to ~7s — is verified manually against real Swift; it can't be
// reproduced deterministically with a synthetic input.)
func TestParseTimeoutBudget(t *testing.T) {
	if tsParseTimeoutMicros <= 0 {
		t.Fatalf("tsParseTimeoutMicros = %d; must be > 0 to bound pathological parses (#432)", tsParseTimeoutMicros)
	}
	// Healthy files parse in milliseconds, so the budget must be far above
	// that (sanity: at least 1s) yet finite.
	if tsParseTimeoutMicros < 1_000_000 {
		t.Errorf("tsParseTimeoutMicros = %d (<1s) risks skipping healthy files", tsParseTimeoutMicros)
	}
	// A normal file still extracts fine under the cap.
	funcs, _, _, _, _, _ := extractTreeSitterSymbols("swift", []byte("func greet() -> String { return \"hi\" }\n"))
	if !slices.Contains(funcs, "greet") {
		t.Errorf("normal swift file should still extract under the parse cap; got %v", funcs)
	}
}

// TestExtractTreeSitterCallEdges checks per-function call attribution
// (caller\x00callee pairs) via span-containment — the data behind calls().
func TestExtractTreeSitterCallEdges(t *testing.T) {
	cases := []struct {
		language string
		src      string
		want     []string // "caller\x00callee" pairs expected
	}{
		{"rust", `fn a() { b(); c(); }
fn d() { e(); }`, []string{"a\x00b", "a\x00c", "d\x00e"}},
		{"typescript", `function a() { b(); c(); }
function d() { e(); }`, []string{"a\x00b", "a\x00c", "d\x00e"}},
		{"javascript", `function a() { b(); c(); }`, []string{"a\x00b", "a\x00c"}},
		{"ruby", "def a\n  c(1)\n  d(2)\nend\n", []string{"a\x00c", "a\x00d"}},
		{"swift", `func a() { b(); c() }`, []string{"a\x00b", "a\x00c"}},
		{"kotlin", `fun a() { b(); c() }`, []string{"a\x00b", "a\x00c"}},
		{"c", `int a() { b(); c(); return 0; }`, []string{"a\x00b", "a\x00c"}},
		{"cpp", `int a() { b(); c(); return 0; }`, []string{"a\x00b", "a\x00c"}},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			_, _, _, _, edges, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "call_edges", edges, tc.want)
		})
	}
}

// TestExtractTreeSitterSymbols_Concurrent exercises the same language
// from many goroutines — the production walker calls extractors from N
// workers concurrently. Run with -race to catch shared-state bugs in
// the ParserPool / Query reuse.
func TestExtractTreeSitterSymbols_Concurrent(t *testing.T) {
	src := []byte(`use std::fmt;
pub struct A;
pub fn f() {}
pub fn g() {}
`)
	const goroutines = 16
	done := make(chan bool, goroutines)
	for range goroutines {
		go func() {
			funcs, types, imports, _, _, _ := extractTreeSitterSymbols("rust", src)
			done <- len(funcs) == 2 && len(types) == 1 && len(imports) == 1
		}()
	}
	for range goroutines {
		if !<-done {
			t.Error("concurrent extraction returned unexpected counts")
		}
	}
}

func TestExtractTreeSitterSymbols_UnknownLanguage(t *testing.T) {
	f, ty, im, _, _, _ := extractTreeSitterSymbols("brainfuck", []byte("+++."))
	if f != nil || ty != nil || im != nil {
		t.Errorf("expected all-nil for unsupported language, got %v %v %v", f, ty, im)
	}
}

// TestExtractTreeSitterRequireImports pins CommonJS require() import
// extraction for JS/TS — previously only ESM `import` was captured, so
// the large require()-based half of the ecosystem reported zero imports.
func TestExtractTreeSitterRequireImports(t *testing.T) {
	for _, lang := range []string{"javascript", "typescript"} {
		t.Run(lang, func(t *testing.T) {
			src := `const foo = require("./foo");
const bar = require("bar");
import { Baz } from "./baz";`
			_, _, imports, _, _, _ := extractTreeSitterSymbols(lang, []byte(src))
			checkContains(t, "imports", imports, []string{"./foo", "bar", "./baz"})
		})
	}
}

// TestExtractTreeSitterComplexity checks cyclomatic complexity per
// function: a function with nested if/for/if should score 1+3 = 4.
func TestExtractTreeSitterComplexity(t *testing.T) {
	cases := []struct {
		language string
		src      string
	}{
		{"rust", `fn a(x: i32) { if x > 0 { for i in 0..x { if i > 1 {} } } }`},
		{"typescript", `function a(x: number) { if (x > 0) { for (;;) { if (x > 1) {} } } }`},
		{"javascript", `function a(x) { if (x > 0) { for (;;) { if (x > 1) {} } } }`},
		{"ruby", "def a\n  if x then\n    while y do\n      if z then\n      end\n    end\n  end\nend\n"},
		{"swift", `func a() { if x { for i in y { if z {} } } }`},
		{"kotlin", `fun a() { if (x) { for (i in y) { if (z) {} } } }`},
		{"c", `int a(int x) { if (x) { for (;;) { if (x) {} } } return 0; }`},
		{"cpp", `int a(int x) { if (x) { for (;;) { if (x) {} } } return 0; }`},
		// #365-migrated languages whose complexity was silently 0: C#
		// regressed on a typo'd decision node ("for_each_statement"
		// instead of "foreach_statement"), which made the whole decision
		// query fail to compile; PHP/Perl/R carried no function spans.
		// if/for/if (or 3× if for Perl, whose decision query has no loop
		// node) all score 1+3 = 4.
		{"csharp", `class C { void a(int x) { if (x>0) { for (int i=0;i<x;i++) { if (i>1) {} } } } }`},
		{"php", "<?php function a($x) { if ($x) { for ($i=0;$i<$x;$i++) { if ($i>1) {} } } }"},
		{"perl", "sub a { if ($x) { if ($y) { if ($z) { } } } }"},
		{"r", "a <- function(x) { if (x>0) { for (i in 1:x) { if (i>1) {} } } }"},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			_, _, _, _, _, rows := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			cx := ""
			for _, r := range rows {
				p := splitNUL(r)
				if len(p) >= 2 && p[0] == "a" {
					cx = p[1]
				}
			}
			if cx != "4" {
				t.Errorf("%s: complexity of a()=%q want 4; rows=%v", tc.language, cx, rows)
			}
		})
	}
}

// TestRelativeImports covers the relative-import extraction (dots preserved)
// that backs Python package-level coupling (#467).
func TestRelativeImports(t *testing.T) {
	got := relativeImports("python", []byte(
		"import os\nfrom mypkg.svc import thing\nfrom . import cli\nfrom .ctx import App\nfrom ..sansio import x\n"))
	want := map[string]bool{".": true, ".ctx": true, "..sansio": true}
	if len(got) != len(want) {
		t.Fatalf("relativeImports = %v, want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected relative import %q (absolute imports must not appear here)", g)
		}
	}
	// Languages with no relative-import query return nothing.
	if r := relativeImports("go", []byte("package main\nimport \"fmt\"\n")); r != nil {
		t.Errorf("go relativeImports = %v, want nil", r)
	}
}

// TestDeclaredPackage covers the file's declared package/namespace
// extraction that backs package-level coupling (#467).
func TestDeclaredPackage(t *testing.T) {
	cases := []struct {
		name, language, src, want string
	}{
		{"java multi-segment", "java", "package com.foo.bar;\n\npublic class X {}\n", "com.foo.bar"},
		{"java single-segment", "java", "package app;\n\nclass X {}\n", "app"},
		{"java default package", "java", "public class X {}\n", ""},
		{"csharp block namespace", "csharp", "namespace My.App {\n  class X {}\n}\n", "My.App"},
		{"csharp file-scoped namespace", "csharp", "namespace My.Core;\nclass X {}\n", "My.Core"},
		{"csharp no namespace", "csharp", "class X {}\n", ""},
		{"kotlin package", "kotlin", "package com.foo.bar\n\nclass X\n", "com.foo.bar"},
		{"kotlin no package", "kotlin", "class X\n", ""},
		{"scala package", "scala", "package com.foo.bar\n\nobject X\n", "com.foo.bar"},
		{"php namespace", "php", "<?php\nnamespace App\\Foo;\nclass X {}\n", "App\\Foo"},
		{"php no namespace", "php", "<?php\nclass X {}\n", ""},
		{"go has no package query", "go", "package main\n", ""},
		{"rust has no package query", "rust", "pub fn f() {}\n", ""},
		{"unwired language", "brainfuck", "+++.", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := declaredPackage(tc.language, []byte(tc.src)); got != tc.want {
				t.Errorf("declaredPackage(%q) = %q, want %q", tc.language, got, tc.want)
			}
		})
	}
}

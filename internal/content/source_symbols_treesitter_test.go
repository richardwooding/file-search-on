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
			funcs, types, imports, _ := extractTreeSitterSymbols(tc.language, []byte(tc.src))
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
			_, _, _, refs := extractTreeSitterSymbols(tc.language, []byte(tc.src))
			checkContains(t, "references", refs, tc.wantRefs)
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
			funcs, types, imports, _ := extractTreeSitterSymbols("rust", src)
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
	f, ty, im, _ := extractTreeSitterSymbols("brainfuck", []byte("+++."))
	if f != nil || ty != nil || im != nil {
		t.Errorf("expected all-nil for unsupported language, got %v %v %v", f, ty, im)
	}
}

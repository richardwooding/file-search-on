package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractPythonSymbols_Simple(t *testing.T) {
	src := []byte(`from requests import get
import json
import logging as log

def fetch(url):
    return get(url).json()

async def fetch_async(url):
    return await something(url)

class Client:
    def call(self):
        pass
`)
	funcs, types, imports := extractPythonSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !reflect.DeepEqual(funcs, []string{"call", "fetch", "fetch_async"}) {
		t.Errorf("functions = %v", funcs)
	}
	if !reflect.DeepEqual(types, []string{"Client"}) {
		t.Errorf("types = %v", types)
	}
	if !reflect.DeepEqual(imports, []string{"json", "logging", "requests"}) {
		t.Errorf("imports = %v", imports)
	}
}

func TestExtractPythonSymbols_MultiImport(t *testing.T) {
	src := []byte(`import os, sys, json
import foo.bar.baz as fbb
from typing import List, Optional
`)
	_, _, imports := extractPythonSymbols(src)
	sort.Strings(imports)
	want := []string{"foo.bar.baz", "json", "os", "sys", "typing"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractPythonSymbols_NestedDef(t *testing.T) {
	src := []byte(`class Foo:
    def method_a(self):
        def inner():
            pass
        return inner

    class Inner:
        def deeply_nested(self): pass
`)
	funcs, types, _ := extractPythonSymbols(src)
	if !contains(funcs, "method_a") || !contains(funcs, "inner") || !contains(funcs, "deeply_nested") {
		t.Errorf("expected all nested defs, got %v", funcs)
	}
	if !contains(types, "Foo") || !contains(types, "Inner") {
		t.Errorf("expected Foo + Inner in types, got %v", types)
	}
}

func TestExtractPythonSymbols_TypeCheckingImport(t *testing.T) {
	// Imports inside `if TYPE_CHECKING:` still surface — they ARE
	// imports in the file, just conditional at runtime. Documented
	// limitation.
	src := []byte(`from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Iterator
`)
	_, _, imports := extractPythonSymbols(src)
	if !contains(imports, "typing") || !contains(imports, "collections.abc") {
		t.Errorf("expected both imports, got %v", imports)
	}
}

func TestExtractPythonSymbols_TrailingComment(t *testing.T) {
	src := []byte(`import os  # for path joining
`)
	_, _, imports := extractPythonSymbols(src)
	if !reflect.DeepEqual(imports, []string{"os"}) {
		t.Errorf("trailing comment should be stripped, got %v", imports)
	}
}

func TestExtractPythonSymbols_DocstringNotMatched(t *testing.T) {
	// A docstring that LOOKS like a def line — we're best-effort and
	// will match this. Documented limitation; the test locks the
	// known behaviour in so a future fix is intentional.
	src := []byte(`"""Module docstring.

This module has a def foo function.
"""
def real_func(): pass
`)
	funcs, _, _ := extractPythonSymbols(src)
	if !contains(funcs, "real_func") {
		t.Errorf("real_func missing: %v", funcs)
	}
	// The docstring "def foo" line uses 4-space indent of "This module..."
	// but doesn't begin with `def` after whitespace stripping. Verify
	// that the regex doesn't false-positive on it.
	for _, f := range funcs {
		if f == "foo" {
			t.Errorf("false positive: docstring matched as def foo (got %v)", funcs)
		}
	}
}

func TestExtractPythonSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractPythonSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestIsValidPythonModuleName(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"os", true},
		{"foo.bar", true},
		{"foo_bar", true},
		{"_private", true},
		{"", false},
		{".leading", false},
		{"123start", false},
		{"foo.bar.baz", true},
		{"foo bar", false},
		{"foo()", false},
	} {
		if got := isValidPythonModuleName(c.in); got != c.want {
			t.Errorf("isValidPythonModuleName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

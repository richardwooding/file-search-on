package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractRSymbols_SimpleScript(t *testing.T) {
	src := []byte(`# Header comment
library(dplyr)
library("ggplot2")
require(tidyr)
requireNamespace("data.table")

# Function definitions
greet <- function(name) {
  paste("Hello,", name)
}

square = function(x) {
  x * x
}

# Use it
greet("world")
`)
	funcs, _, imports := extractRSymbols(src)
	sort.Strings(funcs)
	sort.Strings(imports)

	if !reflect.DeepEqual(funcs, []string{"greet", "square"}) {
		t.Errorf("functions = %v, want [greet square]", funcs)
	}
	wantImports := []string{"data.table", "dplyr", "ggplot2", "tidyr"}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Errorf("imports = %v, want %v", imports, wantImports)
	}
}

func TestExtractRSymbols_S4Classes(t *testing.T) {
	src := []byte(`setClass("Point", representation(x = "numeric", y = "numeric"))
setGeneric("distance", function(p) standardGeneric("distance"))
setRefClass("Counter", fields = list(count = "integer"))
`)
	_, types, _ := extractRSymbols(src)
	sort.Strings(types)
	wantTypes := []string{"Counter", "Point", "distance"}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Errorf("types = %v, want %v", types, wantTypes)
	}
}

func TestExtractRSymbols_R6Classes(t *testing.T) {
	src := []byte(`Animal <- R6::R6Class("Animal",
  public = list(
    name = NULL,
    initialize = function(name) {
      self$name <- name
    }
  )
)

Dog <- R6Class("Dog", inherit = Animal)
`)
	_, types, _ := extractRSymbols(src)
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"Animal", "Dog"}) {
		t.Errorf("types = %v, want [Animal Dog]", types)
	}
}

func TestExtractRSymbols_DottedFunctionName(t *testing.T) {
	// R allows `.` in identifiers (e.g. `read.csv`, `as.numeric`).
	src := []byte(`my.helper <- function(x) x + 1
as.tibble = function(d) d
`)
	funcs, _, _ := extractRSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"as.tibble", "my.helper"}) {
		t.Errorf("functions = %v, want [as.tibble my.helper]", funcs)
	}
}

func TestExtractRSymbols_NestedFunction(t *testing.T) {
	src := []byte(`outer <- function(x) {
  inner <- function(y) y * 2
  inner(x)
}
`)
	funcs, _, _ := extractRSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"inner", "outer"}) {
		t.Errorf("functions = %v, want [inner outer]", funcs)
	}
}

func TestExtractRSymbols_TrailingComment(t *testing.T) {
	// Trailing # comment must not break the function-assignment match.
	src := []byte(`f <- function(x) x  # trivial identity
g = function(y) y * 2  # double
`)
	funcs, _, _ := extractRSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"f", "g"}) {
		t.Errorf("functions = %v, want [f g]", funcs)
	}
}

func TestExtractRSymbols_QuotedLibraryArg(t *testing.T) {
	// library() accepts the name as a bareword OR quoted string.
	src := []byte(`library(dplyr)
library("ggplot2")
library('tidyr')
require(MASS)
`)
	_, _, imports := extractRSymbols(src)
	sort.Strings(imports)
	want := []string{"MASS", "dplyr", "ggplot2", "tidyr"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractRSymbols_R6DoesNotLeakLHSFunction(t *testing.T) {
	// `Foo <- R6Class("Foo", ...)` — the LHS `Foo` must not appear
	// in functions (it's a class, not a function). Only `type_names`
	// gets the quoted name.
	src := []byte(`Foo <- R6Class("Foo", public = list(hello = function() "hi"))
realfn <- function(x) x
`)
	funcs, types, _ := extractRSymbols(src)
	if !reflect.DeepEqual(funcs, []string{"realfn"}) {
		t.Errorf("functions = %v, want [realfn] only (Foo is a class)", funcs)
	}
	if !reflect.DeepEqual(types, []string{"Foo"}) {
		t.Errorf("types = %v, want [Foo]", types)
	}
}

func TestExtractRSymbols_SuperAssignment(t *testing.T) {
	// `<<-` super-assignment — the regex captures `<-` substring so
	// this matches. Documented behaviour.
	src := []byte(`make_counter <- function() {
  count <- 0
  list(
    inc = function() count <<- count + 1,
    get = function() count
  )
}
`)
	funcs, _, _ := extractRSymbols(src)
	if !contains(funcs, "make_counter") {
		t.Errorf("expected make_counter: %v", funcs)
	}
}

func TestExtractRSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractRSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractRSymbols_NoFalsePositiveOnNonFunctionAssignment(t *testing.T) {
	// `x <- 1:10` is NOT a function assignment. The regex requires
	// the RHS to start with `function` (then optional whitespace
	// then `(`).
	src := []byte(`x <- 1:10
y = c(1, 2, 3)
z <- list(a = 1, b = 2)
`)
	funcs, _, _ := extractRSymbols(src)
	if len(funcs) != 0 {
		t.Errorf("non-function assignments should not match, got %v", funcs)
	}
}

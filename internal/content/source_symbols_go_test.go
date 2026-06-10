package content

import (
	"reflect"
	"slices"
	"sort"
	"testing"
)

func TestExtractGoSymbols_Simple(t *testing.T) {
	src := []byte(`package main

import (
	"fmt"
	"net/http"
)

type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

func ProcessOrder(id string) error {
	fmt.Println(id)
	return nil
}
`)
	funcs, types, imports, _ := extractGoSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !reflect.DeepEqual(funcs, []string{"ProcessOrder", "ServeHTTP"}) {
		t.Errorf("functions = %v", funcs)
	}
	if !reflect.DeepEqual(types, []string{"Handler"}) {
		t.Errorf("types = %v", types)
	}
	if !reflect.DeepEqual(imports, []string{"fmt", "net/http"}) {
		t.Errorf("imports = %v", imports)
	}
}

func TestExtractGoSymbols_AliasedAndDotImports(t *testing.T) {
	src := []byte(`package main

import (
	gohttp "net/http"
	. "fmt"
	_ "embed"
)
`)
	_, _, imports, _ := extractGoSymbols(src)
	sort.Strings(imports)
	want := []string{"embed", "fmt", "net/http"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractGoSymbols_GenericTypesAndInterfaces(t *testing.T) {
	src := []byte(`package main

type Comparable[T any] interface {
	Compare(other T) int
}

type Pair[A, B any] struct {
	First  A
	Second B
}

func Map[T, U any](items []T, fn func(T) U) []U { return nil }
`)
	funcs, types, _, _ := extractGoSymbols(src)
	if !contains(funcs, "Map") {
		t.Errorf("expected Map in functions, got %v", funcs)
	}
	if !contains(types, "Comparable") || !contains(types, "Pair") {
		t.Errorf("expected Comparable + Pair in types, got %v", types)
	}
}

func TestExtractGoSymbols_PartialRecoveryOnBrokenFile(t *testing.T) {
	// Truncated body — parser still recovers package + imports + the
	// first valid decl.
	src := []byte(`package main

import "fmt"

type Foo struct {
	Bar int
}

func Working() {
	fmt.Println("ok")
}

func Broken( {  // syntax error here
`)
	funcs, types, imports, _ := extractGoSymbols(src)
	if !contains(imports, "fmt") {
		t.Errorf("imports should include fmt despite parse error, got %v", imports)
	}
	if !contains(types, "Foo") {
		t.Errorf("types should include Foo despite parse error, got %v", types)
	}
	if !contains(funcs, "Working") {
		t.Errorf("functions should include Working despite parse error, got %v", funcs)
	}
}

func TestExtractGoSymbols_Empty(t *testing.T) {
	funcs, types, imports, _ := extractGoSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractGoSymbols_PackageOnly(t *testing.T) {
	funcs, types, imports, _ := extractGoSymbols([]byte("package main\n"))
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("package-only file should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func contains(slice []string, want string) bool {
	return slices.Contains(slice, want)
}

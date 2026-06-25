package content

import (
	"reflect"
	"slices"
	"sort"
	"strings"
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
	funcs, types, imports, _, _, _ := extractGoSymbols(src)
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
	_, _, imports, _, _, _ := extractGoSymbols(src)
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
	funcs, types, _, _, _, _ := extractGoSymbols(src)
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
	funcs, types, imports, _, _, _ := extractGoSymbols(src)
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
	funcs, types, imports, _, _, _ := extractGoSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractGoSymbols_PackageOnly(t *testing.T) {
	funcs, types, imports, _, _, _ := extractGoSymbols([]byte("package main\n"))
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("package-only file should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func contains(slice []string, want string) bool {
	return slices.Contains(slice, want)
}

// TestExtractGoSymbols_ValueRefs pins issue #421: a function/method used as
// a VALUE (passed as a call argument) must appear in references — so a
// handler registered via a callback isn't a dead_code false positive — but
// must NOT become a call edge (passing a function isn't calling it).
// TestGoHandlerBoundary pins issue #504: the boundary extractor emits the
// value-ref of a handler passed to a call AND the exported types in that
// handler's signature, so unused_exports can exempt AddTool[In,Out]-bound
// types from the package-local false positive.
func TestGoHandlerBoundary(t *testing.T) {
	src := []byte(`package srv

import "context"

type Req struct{ A int }
type Resp struct{ B int }

func register(s any) { AddTool(s, handle) }

func handle(ctx context.Context, in Req) (Resp, error) { return Resp{}, nil }
`)
	got := goHandlerBoundary(src)
	have := map[string]bool{}
	for _, e := range got {
		have[e] = true
	}
	for _, want := range []string{"v\x00handle", "s\x00handle\x00Req", "s\x00handle\x00Resp"} {
		if !have[want] {
			t.Errorf("goHandlerBoundary missing %q; got %v", want, got)
		}
	}
	if have["s\x00register\x00Req"] {
		t.Errorf("register has no Req in its signature; unexpected entry: %v", got)
	}
}

func TestExtractGoSymbols_ValueRefs(t *testing.T) {
	src := []byte(`package p

func register() {
	add("name", handleThing)   // bare func value
	mux.Handle("/x", h.serveIt) // method value
}
func handleThing() {}
`)
	_, _, _, refs, edges, _ := extractGoSymbols(src)
	for _, want := range []string{"handleThing", "serveIt"} {
		if !contains(refs, want) {
			t.Errorf("references missing value-passed %q: %v", want, refs)
		}
	}
	// Passing a function is not calling it — no caller→callee edge to it.
	for _, e := range edges {
		if strings.HasSuffix(e, "\x00handleThing") || strings.HasSuffix(e, "\x00serveIt") {
			t.Errorf("value-passed function leaked into call_edges: %q", e)
		}
	}
}

// TestExtractGoSymbols_TypeUsages pins issue #398: types used only in type
// positions (never called) must appear in references so dead_code stops
// flagging them. Predeclared types must NOT leak in, and the call graph
// must stay call-only.
func TestExtractGoSymbols_TypeUsages(t *testing.T) {
	src := []byte(`package p

type Widget struct{}
type Gadget struct{}
type Sprocket struct{}
type Cog struct{}
type Embedded struct{}

type Holder struct {
	Embedded            // embedded type
	W       Widget      // field type
	G       []*Gadget   // slice-of-pointer field
	M       map[string]Sprocket
}

func use(c Cog) Widget {           // param + result type
	_ = Holder{}                   // composite-literal type
	return Widget{}
}
`)
	_, _, _, refs, edges, _ := extractGoSymbols(src)
	for _, want := range []string{"Widget", "Gadget", "Sprocket", "Cog", "Embedded", "Holder"} {
		if !contains(refs, want) {
			t.Errorf("references missing type usage %q: %v", want, refs)
		}
	}
	// Predeclared types must be filtered out.
	for _, bad := range []string{"string", "error", "int"} {
		if contains(refs, bad) {
			t.Errorf("predeclared %q must not appear in references: %v", bad, refs)
		}
	}
	// Type usages must never become call edges (the call graph is call-only).
	for _, e := range edges {
		for _, ty := range []string{"Widget", "Gadget", "Holder", "Cog"} {
			if len(e) > len(ty) && e[len(e)-len(ty):] == ty && e[:len("use\x00")] == "use\x00" {
				// only fail if it's actually an edge to a *type* — `use` calls nothing here
				t.Errorf("type usage leaked into call_edges: %q", e)
			}
		}
	}
}

func TestExtractGoSymbols_CallEdges(t *testing.T) {
	src := []byte(`package p

func Alpha() { Beta(); helper.Do() }
func Beta()  {}
`)
	_, _, _, refs, edges, _ := extractGoSymbols(src)
	if !contains(refs, "Beta") || !contains(refs, "Do") {
		t.Errorf("references missing Beta/Do: %v", refs)
	}
	for _, want := range []string{"Alpha\x00Beta", "Alpha\x00Do"} {
		if !contains(edges, want) {
			t.Errorf("call_edges missing %q: %v", want, edges)
		}
	}
	// Beta calls nothing — no edge with Beta as caller.
	for _, e := range edges {
		if len(e) >= 5 && e[:5] == "Beta\x00" {
			t.Errorf("unexpected edge from Beta: %q", e)
		}
	}
}

func TestExtractGoSymbols_Complexity(t *testing.T) {
	src := []byte(`package p

func Simple() {}
func Branchy(x int) {
	if x > 0 {
		for i := 0; i < x; i++ {
			if i%2 == 0 && x > 5 {
			}
		}
	}
}
`)
	_, _, _, _, _, rows := extractGoSymbols(src)
	cyclomatic := map[string]string{}
	cognitive := map[string]string{}
	for _, r := range rows {
		p := splitNUL(r)
		cyclomatic[p[0]] = p[1] // name -> cyclomatic
		if len(p) >= 5 {
			cognitive[p[0]] = p[4] // name -> cognitive (#485)
		}
	}
	if cyclomatic["Simple"] != "1" {
		t.Errorf("Simple complexity=%q want 1", cyclomatic["Simple"])
	}
	// Branchy: base 1 + if + for + if + && = 5.
	if cyclomatic["Branchy"] != "5" {
		t.Errorf("Branchy complexity=%q want 5; rows=%v", cyclomatic["Branchy"], rows)
	}
	// Cognitive: Simple is flat (0); Branchy nests if(+1) > for(+2) > if(+3)
	// plus one && run (+1) = 7 — higher than its cyclomatic, reflecting depth.
	if cognitive["Simple"] != "0" {
		t.Errorf("Simple cognitive=%q want 0", cognitive["Simple"])
	}
	if cognitive["Branchy"] != "7" {
		t.Errorf("Branchy cognitive=%q want 7; rows=%v", cognitive["Branchy"], rows)
	}
}

func splitNUL(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == 0 {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

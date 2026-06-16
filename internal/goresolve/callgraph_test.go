package goresolve

import (
	"slices"
	"testing"
)

// TestCallers_TypePrecise: who_calls on a method resolves to the exact type.
// A.Foo and B.Foo are distinct; callers of A.Foo must not include the caller
// of B.Foo (the case name-based matching conflates).
func TestCallers_TypePrecise(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": `package a

type A struct{}
type B struct{}

func (A) Foo() {}
func (B) Foo() {}

func CallA() { var x A; x.Foo() }
func CallB() { var y B; y.Foo() }
`,
	})
	res, ok, err := Resolve(t.Context(), dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok {
		t.Skip("type resolution unavailable")
	}

	callerNames := func(owner, name string) []string {
		var out []string
		for _, s := range res.Callers(owner, name) {
			out = append(out, s.Caller)
		}
		slices.Sort(out)
		return out
	}
	if got := callerNames("A", "Foo"); !slices.Equal(got, []string{"CallA"}) {
		t.Errorf("Callers(A.Foo)=%v, want [CallA]", got)
	}
	if got := callerNames("B", "Foo"); !slices.Equal(got, []string{"CallB"}) {
		t.Errorf("Callers(B.Foo)=%v, want [CallB]", got)
	}
}

// TestImpact_TransitiveClosure: impact follows callers transitively.
// leaf <- mid <- top; impact(leaf) = {mid, top}.
func TestImpact_TransitiveClosure(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": `package a

func leaf() {}
func mid()  { leaf() }
func top()  { mid() }
func Entry() { top() }
`,
	})
	res, ok, err := Resolve(t.Context(), dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok {
		t.Skip("type resolution unavailable")
	}
	var names []string
	for _, s := range res.Impact("", "leaf") {
		names = append(names, s.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Entry", "mid", "top"}) {
		t.Errorf("Impact(leaf)=%v, want [Entry mid top]", names)
	}
}

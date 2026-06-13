package content

import (
	"testing"
)

func TestReferenceLines_Go(t *testing.T) {
	src := []byte("package p\n" + // line 1
		"\n" + // 2
		"type Widget struct{}\n" + // 3 — definition, NOT a reference
		"\n" + // 4
		"func use(w Widget) Widget {\n" + // 5 — param + return type usages
		"\treturn make(w)\n" + // 6 — call to make
		"}\n" + // 7
		"\n" + // 8
		"func wire() {\n" + // 9
		"\tregister(use)\n" + // 10 — `use` passed as a value
		"}\n") // 11

	got := ReferenceLines("go", src, "Widget")
	// Two type usages on line 5 (param + return) — same line, same kind →
	// deduped to one site.
	if len(got) != 1 || got[0].Line != 5 || got[0].Kind != "type" {
		t.Errorf("Widget references = %+v, want one {5, type}", got)
	}

	useRefs := ReferenceLines("go", src, "use")
	if len(useRefs) != 1 || useRefs[0].Line != 10 || useRefs[0].Kind != "value" {
		t.Errorf("use references = %+v, want one {10, value}", useRefs)
	}

	makeRefs := ReferenceLines("go", src, "make")
	if len(makeRefs) != 1 || makeRefs[0].Line != 6 || makeRefs[0].Kind != "call" {
		t.Errorf("make references = %+v, want one {6, call}", makeRefs)
	}

	// A symbol that never appears yields nothing.
	if r := ReferenceLines("go", src, "Nonexistent"); r != nil {
		t.Errorf("unreferenced symbol should yield nil, got %+v", r)
	}
}

func TestReferenceLines_TreeSitter(t *testing.T) {
	// Rust: Widget used as a field type (line 2) and a parameter type (line 4).
	src := []byte("struct Holder {\n" + // 1
		"\tw: Widget,\n" + // 2 — field type
		"}\n" + // 3
		"fn build(c: Widget) {}\n") // 4 — parameter type

	got := ReferenceLines("rust", src, "Widget")
	lines := map[int]string{}
	for _, s := range got {
		lines[s.Line] = s.Kind
	}
	if lines[2] != "type" || lines[4] != "type" {
		t.Errorf("Rust Widget references = %+v, want type usages on lines 2 and 4", got)
	}
}

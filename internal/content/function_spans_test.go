package content

import "testing"

func spanByName(spans []FunctionSpan, name string) (FunctionSpan, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return FunctionSpan{}, false
}

func TestFunctionSpans_Go(t *testing.T) {
	src := []byte("package demo\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func add(a, b int) int {\n" + // 5
		"\treturn a + b\n" + // 6
		"}\n" + // 7
		"\n" + // 8
		"func greet() {\n" + // 9
		"\tfmt.Println(\"hi\")\n" + // 10
		"}\n") // 11

	spans := FunctionSpans("source/go", src)
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	add, ok := spanByName(spans, "add")
	if !ok {
		t.Fatal("missing span for add")
	}
	if add.StartLine != 5 || add.EndLine != 7 {
		t.Errorf("add span = %d-%d, want 5-7", add.StartLine, add.EndLine)
	}
	greet, ok := spanByName(spans, "greet")
	if !ok {
		t.Fatal("missing span for greet")
	}
	if greet.StartLine != 9 || greet.EndLine != 11 {
		t.Errorf("greet span = %d-%d, want 9-11", greet.StartLine, greet.EndLine)
	}
}

func TestFunctionSpans_TreeSitter_Rust(t *testing.T) {
	src := []byte("fn add(a: i32, b: i32) -> i32 {\n" + // 1
		"    a + b\n" + // 2
		"}\n" + // 3
		"\n" + // 4
		"fn main() {\n" + // 5
		"    println!(\"{}\", add(1, 2));\n" + // 6
		"}\n") // 7

	spans := FunctionSpans("source/rust", src)
	if _, ok := spanByName(spans, "add"); !ok {
		t.Errorf("missing span for add: %+v", spans)
	}
	main, ok := spanByName(spans, "main")
	if !ok {
		t.Fatalf("missing span for main: %+v", spans)
	}
	if main.StartLine != 5 || main.EndLine != 7 {
		t.Errorf("main span = %d-%d, want 5-7", main.StartLine, main.EndLine)
	}
}

func TestFunctionSpans_NotSource(t *testing.T) {
	if spans := FunctionSpans("text/plain", []byte("hello")); spans != nil {
		t.Errorf("non-source type should yield nil, got %+v", spans)
	}
	if spans := FunctionSpans("source/unwired", []byte("whatever")); spans != nil {
		t.Errorf("unwired language should yield nil, got %+v", spans)
	}
}

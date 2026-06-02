package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractCSharpSymbols_SimpleClass(t *testing.T) {
	src := []byte(`using System;
using System.Collections.Generic;
using static System.Math;
using Tasks = System.Threading.Tasks;

namespace MyApp;

public class OrderService {
    private readonly List<Order> orders;

    public OrderService(List<Order> orders) {
        this.orders = orders;
    }

    public Order ProcessOrder(string id) {
        return orders[0];
    }

    private void Log(string msg) {
        System.Console.WriteLine(msg);
    }
}
`)
	funcs, types, imports := extractCSharpSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !contains(types, "OrderService") {
		t.Errorf("types missing OrderService: %v", types)
	}
	// The ctor `public OrderService(List<Order> orders)` is method-
	// shaped to the regex — that's fine (constructors are valid
	// functions; agents looking up `OrderService` find it in
	// functions AND type_names).
	if !contains(funcs, "ProcessOrder") {
		t.Errorf("functions missing ProcessOrder: %v", funcs)
	}
	if !contains(funcs, "Log") {
		t.Errorf("functions missing Log: %v", funcs)
	}
	wantImports := []string{
		"System",
		"System.Collections.Generic",
		"System.Math",
		"System.Threading.Tasks",
	}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Errorf("imports = %v, want %v", imports, wantImports)
	}
}

func TestExtractCSharpSymbols_RecordsAndEnums(t *testing.T) {
	src := []byte(`public record Point(int X, int Y);
public record struct PointStruct(int X, int Y);
public record class PointClass(int X, int Y);

public enum Status { Active, Inactive }

public interface INamed {
    string GetName();
}

public delegate void Handler(int x);
`)
	_, types, _ := extractCSharpSymbols(src)
	for _, want := range []string{"Point", "PointStruct", "PointClass", "Status", "INamed", "Handler"} {
		if !contains(types, want) {
			t.Errorf("types missing %s: %v", want, types)
		}
	}
}

func TestExtractCSharpSymbols_GenericMethod(t *testing.T) {
	src := []byte(`public class Mapper {
    public static U Convert<T, U>(T input) where T : class where U : new() {
        return new U();
    }
}
`)
	funcs, _, _ := extractCSharpSymbols(src)
	if !contains(funcs, "Convert") {
		t.Errorf("expected Convert in functions, got %v", funcs)
	}
}

func TestExtractCSharpSymbols_AsyncAndExpressionBodied(t *testing.T) {
	src := []byte(`public class Api {
    public async Task<int> FetchAsync(string url) {
        return 0;
    }

    public int Square(int x) => x * x;

    public abstract int Pending();
}
`)
	funcs, _, _ := extractCSharpSymbols(src)
	for _, want := range []string{"FetchAsync", "Square", "Pending"} {
		if !contains(funcs, want) {
			t.Errorf("expected %s in functions, got %v", want, funcs)
		}
	}
}

func TestExtractCSharpSymbols_PartialClass(t *testing.T) {
	src := []byte(`public partial class Builder {
    public void Build() {}
}

public partial class Builder {
    public void Append(string s) {}
}
`)
	_, types, _ := extractCSharpSymbols(src)
	// Both partial halves emit a Builder entry — that's fine; agents
	// can dedupe with CEL .filter() if they care.
	count := 0
	for _, n := range types {
		if n == "Builder" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected Builder twice (one per partial decl), got %d: %v", count, types)
	}
}

func TestExtractCSharpSymbols_NestedTypes(t *testing.T) {
	src := []byte(`public class Outer {
    public class Inner {
        public void Hello() {}
    }
    public struct Nested { }
}
`)
	funcs, types, _ := extractCSharpSymbols(src)
	for _, want := range []string{"Outer", "Inner", "Nested"} {
		if !contains(types, want) {
			t.Errorf("types missing %s: %v", want, types)
		}
	}
	if !contains(funcs, "Hello") {
		t.Errorf("functions missing Hello: %v", funcs)
	}
}

func TestExtractCSharpSymbols_KeywordFalsePositiveGuard(t *testing.T) {
	// Control-flow keywords with parens — must NOT be captured as
	// functions. The method-regex requires a modifier keyword to
	// precede the method name, so most are excluded by structure;
	// the keyword guard is the second line of defence.
	src := []byte(`public class Foo {
    public void Bar() {
        if (true) { }
        for (int i = 0; i < 10; i++) { }
        while (false) { }
        switch (1) { case 1: break; }
    }
}
`)
	funcs, _, _ := extractCSharpSymbols(src)
	for _, banned := range []string{"if", "for", "while", "switch"} {
		if contains(funcs, banned) {
			t.Errorf("false-positive: %q matched as a function: %v", banned, funcs)
		}
	}
	if !contains(funcs, "Bar") {
		t.Errorf("functions should contain Bar: %v", funcs)
	}
}

func TestExtractCSharpSymbols_FileScopedNamespace(t *testing.T) {
	src := []byte(`namespace Foo.Bar;

using System;

public class Widget {
    public void Run() {}
}
`)
	funcs, types, imports := extractCSharpSymbols(src)
	if !contains(types, "Widget") {
		t.Errorf("expected Widget in types: %v", types)
	}
	if !contains(funcs, "Run") {
		t.Errorf("expected Run in functions: %v", funcs)
	}
	if !contains(imports, "System") {
		t.Errorf("expected System in imports: %v", imports)
	}
	// The `namespace Foo.Bar;` line should NOT emit a type_name.
	if contains(types, "Foo.Bar") || contains(types, "Foo") {
		t.Errorf("namespace declaration should NOT emit a type_name: %v", types)
	}
}

func TestExtractCSharpSymbols_AbstractAndInterfaceMethods(t *testing.T) {
	// Methods ending in `;` (abstract / interface-method / extern)
	// should match.
	src := []byte(`public abstract class Animal {
    public abstract void Speak();
    public virtual void Move() {}
}
public interface IDoer {
    void Do();
    int Count { get; set; }
}
`)
	funcs, _, _ := extractCSharpSymbols(src)
	for _, want := range []string{"Speak", "Move", "Do"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %s: %v", want, funcs)
		}
	}
}

func TestExtractCSharpSymbols_GlobalAndAlias(t *testing.T) {
	src := []byte(`using global::System;
using IO = System.IO;
`)
	_, _, imports := extractCSharpSymbols(src)
	// `using global::System;` → "System" (the global:: prefix is dropped)
	// `using IO = System.IO;` → "System.IO" (the alias's RHS)
	wantContains := []string{"System", "System.IO"}
	for _, want := range wantContains {
		if !contains(imports, want) {
			t.Errorf("imports missing %s: %v", want, imports)
		}
	}
}

func TestExtractCSharpSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractCSharpSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestIsCSharpKeyword(t *testing.T) {
	for _, k := range []string{"if", "for", "while", "switch", "try", "using", "lock", "await", "yield"} {
		if !isCSharpKeyword(k) {
			t.Errorf("expected %q to be a keyword", k)
		}
	}
	for _, n := range []string{"ProcessOrder", "Calculate", "Main", "MyClass"} {
		if isCSharpKeyword(n) {
			t.Errorf("expected %q to NOT be a keyword", n)
		}
	}
}

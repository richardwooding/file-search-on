package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractJavaSymbols_Simple(t *testing.T) {
	src := []byte(`package com.example;

import java.util.List;
import java.util.Map;
import static java.lang.Math.PI;

public class OrderService {
    private final List<Order> orders;

    public OrderService(List<Order> orders) {
        this.orders = orders;
    }

    public Order processOrder(String id) {
        return orders.get(0);
    }

    private void log(String msg) {
        System.out.println(msg);
    }
}
`)
	funcs, types, imports := extractJavaSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !contains(types, "OrderService") {
		t.Errorf("types missing OrderService: %v", types)
	}
	if !contains(funcs, "processOrder") {
		t.Errorf("functions missing processOrder: %v", funcs)
	}
	if !contains(funcs, "log") {
		t.Errorf("functions missing log: %v", funcs)
	}
	want := []string{"java.lang.Math.PI", "java.util.List", "java.util.Map"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractJavaSymbols_RecordAndEnum(t *testing.T) {
	src := []byte(`package x;

public enum Status { ACTIVE, INACTIVE }

public record Point(int x, int y) {}

public interface Named { String getName(); }

public sealed class Animal permits Dog, Cat {}
`)
	_, types, _ := extractJavaSymbols(src)
	for _, want := range []string{"Status", "Point", "Named", "Animal"} {
		if !contains(types, want) {
			t.Errorf("missing %s in types %v", want, types)
		}
	}
}

func TestExtractJavaSymbols_ImportWildcard(t *testing.T) {
	src := []byte(`import java.util.*;
import com.example.helpers.Utils.*;
`)
	_, _, imports := extractJavaSymbols(src)
	sort.Strings(imports)
	if !contains(imports, "java.util.*") {
		t.Errorf("expected java.util.* in imports: %v", imports)
	}
}

func TestExtractJavaSymbols_NestedClass(t *testing.T) {
	src := []byte(`public class Outer {
    public static class Inner {
        public void hello() {}
    }
}
`)
	funcs, types, _ := extractJavaSymbols(src)
	if !contains(types, "Outer") || !contains(types, "Inner") {
		t.Errorf("expected Outer + Inner in types: %v", types)
	}
	if !contains(funcs, "hello") {
		t.Errorf("expected hello in functions: %v", funcs)
	}
}

func TestExtractJavaSymbols_NoKeywordFalsePositive(t *testing.T) {
	// Confirm `if (`, `for (`, etc. inside a class body do NOT
	// become functions.
	src := []byte(`public class Foo {
    public void bar() {
        if (x) { ... }
        for (int i = 0; i < 10; i++) {}
        while (y) {}
    }
}
`)
	funcs, _, _ := extractJavaSymbols(src)
	for _, banned := range []string{"if", "for", "while"} {
		if contains(funcs, banned) {
			t.Errorf("false-positive: keyword %q matched as function (got %v)", banned, funcs)
		}
	}
}

func TestExtractJavaSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractJavaSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestIsJavaKeyword(t *testing.T) {
	for _, k := range []string{"if", "for", "while", "switch", "return", "throw", "try"} {
		if !isJavaKeyword(k) {
			t.Errorf("expected %q to be a keyword", k)
		}
	}
	for _, n := range []string{"processOrder", "calculate", "main"} {
		if isJavaKeyword(n) {
			t.Errorf("expected %q to NOT be a keyword", n)
		}
	}
}

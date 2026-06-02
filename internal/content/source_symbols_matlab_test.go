package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractMATLABSymbols_SimpleFunction(t *testing.T) {
	src := []byte(`% Square the input.
function y = square(x)
    y = x .* x;
end

function noReturn(z)
    disp(z);
end
`)
	funcs, _, _ := extractMATLABSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"noReturn", "square"}) {
		t.Errorf("functions = %v, want [noReturn square]", funcs)
	}
}

func TestExtractMATLABSymbols_ReturnValueForms(t *testing.T) {
	// Single return, multi-return [a, b], and no-whitespace `out=name(`.
	src := []byte(`function out = single(x)
    out = x;
end

function [a, b] = pair(x)
    a = x;
    b = x + 1;
end

function out=tight(x)
    out = x;
end

function [first, second, third] = triple(x)
    first  = x;
    second = x * 2;
    third  = x * 3;
end
`)
	funcs, _, _ := extractMATLABSymbols(src)
	sort.Strings(funcs)
	want := []string{"pair", "single", "tight", "triple"}
	if !reflect.DeepEqual(funcs, want) {
		t.Errorf("functions = %v, want %v", funcs, want)
	}
}

func TestExtractMATLABSymbols_ClassdefWithInheritance(t *testing.T) {
	src := []byte(`classdef Point
    properties
        x
        y
    end
end

classdef Point3D < Point
    properties
        z
    end
end

classdef Animal < handle & matlab.mixin.Copyable
    properties
        name
    end
end
`)
	_, types, _ := extractMATLABSymbols(src)
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"Animal", "Point", "Point3D"}) {
		t.Errorf("types = %v, want [Animal Point Point3D]", types)
	}
}

func TestExtractMATLABSymbols_ClassdefAttributes(t *testing.T) {
	// (Abstract) / (Sealed) / (Abstract, Hidden) attributes are
	// optional and must NOT be captured as the class name.
	src := []byte(`classdef (Abstract) Shape
end

classdef (Sealed, Hidden) Internal
end

classdef (Abstract = true) Maybe
end
`)
	_, types, _ := extractMATLABSymbols(src)
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"Internal", "Maybe", "Shape"}) {
		t.Errorf("types = %v, want [Internal Maybe Shape]", types)
	}
}

func TestExtractMATLABSymbols_MethodsInsideClass(t *testing.T) {
	// Methods sections of classdef contain function declarations that
	// match the same regex — verify they surface.
	src := []byte(`classdef Counter
    properties
        value = 0
    end
    methods
        function obj = Counter(init)
            obj.value = init;
        end
        function inc(obj)
            obj.value = obj.value + 1;
        end
        function v = get(obj)
            v = obj.value;
        end
    end
end
`)
	funcs, types, _ := extractMATLABSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(types, []string{"Counter"}) {
		t.Errorf("types = %v, want [Counter]", types)
	}
	if !reflect.DeepEqual(funcs, []string{"Counter", "get", "inc"}) {
		t.Errorf("functions = %v, want [Counter get inc]", funcs)
	}
}

func TestExtractMATLABSymbols_NestedFunction(t *testing.T) {
	src := []byte(`function outer(x)
    inner(x);
    function inner(y)
        disp(y);
    end
end
`)
	funcs, _, _ := extractMATLABSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"inner", "outer"}) {
		t.Errorf("functions = %v, want [inner outer]", funcs)
	}
}

func TestExtractMATLABSymbols_Imports(t *testing.T) {
	src := []byte(`import matlab.io.fileread
import matlab.unittest.*
import com.example.MyClass
`)
	_, _, imports := extractMATLABSymbols(src)
	sort.Strings(imports)
	want := []string{"com.example.MyClass", "matlab.io.fileread", "matlab.unittest.*"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractMATLABSymbols_AnonymousFunctionNotMatched(t *testing.T) {
	// f = @(x) x.^2  has no name to capture and must not appear in
	// functions. Only `real_one` should be captured.
	src := []byte(`f = @(x) x.^2;
g = @(x, y) x + y;
function real_one(z)
    disp(z);
end
`)
	funcs, _, _ := extractMATLABSymbols(src)
	if !reflect.DeepEqual(funcs, []string{"real_one"}) {
		t.Errorf("functions = %v, want [real_one] only", funcs)
	}
}

func TestExtractMATLABSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractMATLABSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractMATLABSymbols_TrailingComment(t *testing.T) {
	// A `% comment` after the function signature must not break the match.
	src := []byte(`function y = f(x)  % a one-liner identity
    y = x;
end
`)
	funcs, _, _ := extractMATLABSymbols(src)
	if !reflect.DeepEqual(funcs, []string{"f"}) {
		t.Errorf("functions = %v, want [f]", funcs)
	}
}

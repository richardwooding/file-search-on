package content

import (
	"strconv"
	"strings"
	"testing"
)

// cognitiveByFunc extracts {function name -> cognitive complexity} from the
// 5-field complexity rows the tree-sitter extractor emits (#491). Functions
// whose row has no 5th field (cognitive unavailable) are omitted.
func cognitiveByFunc(t *testing.T, language string, src string) map[string]int {
	t.Helper()
	_, _, _, _, _, rows := extractTreeSitterSymbols(language, []byte(src))
	out := map[string]int{}
	for _, r := range rows {
		p := strings.Split(r, "\x00")
		if len(p) < 5 {
			continue
		}
		n, err := strconv.Atoi(p[4])
		if err != nil {
			continue
		}
		out[p[0]] = n
	}
	return out
}

func TestTSCognitiveComplexity(t *testing.T) {
	cases := []struct {
		name, language, fn, src string
		want                    int
	}{
		{
			name: "python-nested", language: "python", fn: "branchy",
			src: "def branchy(x):\n" +
				"    if x > 0:\n" +
				"        for i in range(x):\n" +
				"            if i % 2 == 0:\n" +
				"                return i\n" +
				"    return 0\n",
			want: 6, // if(1) + for(2) + if(3)
		},
		{
			name: "python-elif", language: "python", fn: "f",
			src: "def f(n):\n" +
				"    if n == 1:\n" +
				"        return 1\n" +
				"    elif n == 2:\n" +
				"        return 2\n" +
				"    else:\n" +
				"        return 3\n",
			want: 3, // if(1) + elif(1) + else(1)
		},
		{
			name: "python-flat", language: "python", fn: "f",
			src: "def f(a, b):\n" +
				"    if a:\n" +
				"        return 1\n" +
				"    if b:\n" +
				"        return 2\n" +
				"    return 0\n",
			want: 2, // two flat ifs — must be below python-nested
		},
		{
			name: "python-bool", language: "python", fn: "f",
			src: "def f(a, b, c):\n" +
				"    if a and b and c:\n" +
				"        return 1\n" +
				"    return 0\n",
			want: 2, // if(1) + one and-run(1)
		},
		{
			name: "js-nested", language: "javascript", fn: "branchy",
			src: "function branchy(x) {\n" +
				"  if (x > 0) {\n" +
				"    for (let i = 0; i < x; i++) {\n" +
				"      if (i % 2 === 0) { return i; }\n" +
				"    }\n" +
				"  }\n" +
				"  return 0;\n" +
				"}\n",
			want: 6,
		},
		{
			name: "js-elseif", language: "javascript", fn: "f",
			src: "function f(n) {\n" +
				"  if (n === 1) { return 1; }\n" +
				"  else if (n === 2) { return 2; }\n" +
				"  else { return 3; }\n" +
				"}\n",
			want: 3, // if(1) + else-if(1) + else(1)
		},
		{
			name: "ts-switch", language: "typescript", fn: "f",
			src: "function f(n: number): string {\n" +
				"  switch (n) {\n" +
				"    case 1: return \"one\";\n" +
				"    case 2: return \"two\";\n" +
				"    default: return \"lots\";\n" +
				"  }\n" +
				"}\n",
			want: 1, // switch is +1, cases free
		},
		{
			name: "java-nested", language: "java", fn: "branchy",
			src: "class C {\n" +
				"  int branchy(int x) {\n" +
				"    if (x > 0) {\n" +
				"      for (int i = 0; i < x; i++) {\n" +
				"        if (i % 2 == 0) { return i; }\n" +
				"      }\n" +
				"    }\n" +
				"    return 0;\n" +
				"  }\n" +
				"}\n",
			want: 6,
		},
		{
			// Classic Java switch_statement (not switch_expression) must count.
			name: "java-switch", language: "java", fn: "f",
			src: "class C {\n" +
				"  String f(int n) {\n" +
				"    switch (n) {\n" +
				"      case 1: return \"one\";\n" +
				"      default: return \"lots\";\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			want: 1, // switch is +1, cases free
		},
		{
			// JS for...of must increment + nest (regression for the missing
			// for_of_statement node type).
			name: "js-forof", language: "javascript", fn: "f",
			src: "function f(xs) {\n" +
				"  for (const x of xs) {\n" +
				"    if (x > 0) { return x; }\n" +
				"  }\n" +
				"  return 0;\n" +
				"}\n",
			want: 3, // for-of(1) + nested if(2)
		},
		{
			name: "rust-nested", language: "rust", fn: "branchy",
			src: "fn branchy(x: i32) -> i32 {\n" +
				"    if x > 0 {\n" +
				"        for i in 0..x {\n" +
				"            if i % 2 == 0 {\n" +
				"                return i;\n" +
				"            }\n" +
				"        }\n" +
				"    }\n" +
				"    return 0;\n" +
				"}\n",
			want: 6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cognitiveByFunc(t, tc.language, tc.src)
			v, ok := got[tc.fn]
			if !ok {
				t.Fatalf("no cognitive value for %q; got rows %+v", tc.fn, got)
			}
			if v != tc.want {
				t.Errorf("cognitive(%s)=%d, want %d", tc.fn, v, tc.want)
			}
		})
	}
}

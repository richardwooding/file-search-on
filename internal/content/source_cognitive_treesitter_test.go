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
		{
			name: "c-nested", language: "c", fn: "branchy",
			src: "int branchy(int x){\n  if(x>0){\n    for(int i=0;i<x;i++){\n      if(i%2==0){ return i; }\n    }\n  }\n  return 0;\n}\n",
			want: 6,
		},
		{
			name: "c-elseif", language: "c", fn: "f",
			src: "int f(int n){\n  if(n==1){ return 1; }\n  else if(n==2){ return 2; }\n  return 0;\n}\n",
			want: 2, // if(1) + else-if(1); trailing plain else not separately counted in C-family
		},
		{
			name: "c-switch", language: "c", fn: "f",
			src: "int f(int n){\n  switch(n){\n    case 1: return 1;\n    default: return 0;\n  }\n}\n",
			want: 1, // switch +1, cases free
		},
		{
			name: "cpp-trycatch", language: "cpp", fn: "f",
			src: "int f(int x){\n  if(x>0){\n    try { g(); } catch(int e){ if(e>0){ return e; } }\n  }\n  return 0;\n}\n",
			want: 6, // if(1) + catch at nesting 1(+2) + if at nesting 2(+3)
		},
		{
			name: "csharp-nested", language: "csharp", fn: "Branchy",
			src: "class C{\n  int Branchy(int x){\n    if(x>0){\n      for(int i=0;i<x;i++){\n        if(i%2==0){ return i; }\n      }\n    }\n    return 0;\n  }\n}\n",
			want: 6,
		},
		{
			name: "csharp-elseif", language: "csharp", fn: "F",
			src: "class C{\n  int F(int n){\n    if(n==1){ return 1; }\n    else if(n==2){ return 2; }\n    return 0;\n  }\n}\n",
			want: 2,
		},
		{
			// Chained else-if: each branch is flat — if + 3×else-if = 4, no
			// nesting penalty accumulating down the chain (regression for
			// re-tagging the else-if's own else branch).
			name: "csharp-elseif-chain", language: "csharp", fn: "F",
			src: "class C{\n  int F(int n){\n    if(n==1){ return 1; }\n    else if(n==2){ return 2; }\n    else if(n==3){ return 3; }\n    else if(n==4){ return 4; }\n    return 0;\n  }\n}\n",
			want: 4,
		},
		{
			// Python chained elif via distinct elif_clause nodes: if + 3×elif = 4.
			name: "python-elif-chain", language: "python", fn: "f",
			src: "def f(n):\n" +
				"    if n == 1:\n        return 1\n" +
				"    elif n == 2:\n        return 2\n" +
				"    elif n == 3:\n        return 3\n" +
				"    elif n == 4:\n        return 4\n" +
				"    return 0\n",
			want: 4,
		},
		{
			name: "kotlin-nested", language: "kotlin", fn: "branchy",
			src: "fun branchy(x: Int): Int {\n  if (x > 0) {\n    for (i in 0..x) {\n      if (i % 2 == 0) { return i }\n    }\n  }\n  return 0\n}\n",
			want: 6,
		},
		{
			name: "kotlin-when", language: "kotlin", fn: "f",
			src: "fun f(n: Int): Int {\n  return when (n) {\n    1 -> 1\n    2 -> 2\n    else -> 0\n  }\n}\n",
			want: 1, // when +1, entries free
		},
		{
			name: "php-nested", language: "php", fn: "branchy",
			src: "<?php function branchy($x){\n  if($x>0){\n    foreach($xs as $i){\n      if($i%2==0){ return $i; }\n    }\n  }\n  return 0;\n}\n",
			want: 6,
		},
		{
			name: "php-elseif", language: "php", fn: "f",
			src: "<?php function f($n){\n  if($n==1){ return 1; }\n  elseif($n==2){ return 2; }\n  else { return 3; }\n  return 0;\n}\n",
			want: 3, // php else_if_clause + else_clause are distinct flat nodes
		},
		{
			name: "ruby-nested", language: "ruby", fn: "branchy",
			src: "def branchy(x)\n  if x > 0\n    while x > 0\n      if x > 5\n        x -= 1\n      end\n    end\n  end\n  0\nend\n",
			want: 6,
		},
		{
			name: "ruby-case", language: "ruby", fn: "f",
			src: "def f(x)\n  case x\n  when 1 then 1\n  else 0\n  end\nend\n",
			want: 1, // case +1; when/else free (else must not double-count)
		},
		{
			name: "ruby-elsif", language: "ruby", fn: "f",
			src: "def f(n)\n  if n == 1\n    1\n  elsif n == 2\n    2\n  end\nend\n",
			want: 2, // if(1) + elsif(1)
		},
		{
			name: "scala-nested", language: "scala", fn: "branchy",
			src: "def branchy(x: Int): Int = {\n  if (x > 0) {\n    for (i <- 0 until x) {\n      if (i % 2 == 0) return i\n    }\n  }\n  0\n}\n",
			want: 6,
		},
		{
			name: "scala-match", language: "scala", fn: "f",
			src: "def f(x: Int): Int = {\n  x match {\n    case 1 => 1\n    case _ => 0\n  }\n}\n",
			want: 1, // match +1; case_clauses free
		},
		{
			name: "r-nested", language: "r", fn: "branchy",
			src: "branchy <- function(x) {\n  if (x > 0) {\n    for (i in 1:x) {\n      if (i %% 2 == 0) return(i)\n    }\n  }\n  0\n}\n",
			want: 6,
		},
		{
			name: "matlab-nested", language: "matlab", fn: "branchy",
			src: "function r = branchy(x)\n  r = 0;\n  if x > 0\n    for i = 1:x\n      if mod(i,2) == 0\n        r = i;\n      end\n    end\n  end\nend\n",
			want: 6,
		},
		{
			name: "matlab-elseif", language: "matlab", fn: "f",
			src: "function r = f(n)\n  if n == 1\n    r = 1;\n  elseif n == 2\n    r = 2;\n  else\n    r = 3;\n  end\nend\n",
			want: 3, // matlab elseif_clause + else_clause are distinct flat nodes
		},
		{
			name: "perl-nested", language: "perl", fn: "branchy",
			src: "sub branchy {\n  my $x = shift;\n  if ($x > 0) {\n    while ($x > 0) {\n      if ($x > 5) { $x--; }\n    }\n  }\n  return 0;\n}\n",
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

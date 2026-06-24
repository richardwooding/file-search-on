package content

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// firstFunc parses src and returns its first top-level function declaration.
func firstFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("no function in source")
	return nil
}

func TestGoCognitiveComplexity(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// SonarSource reference: nested loops + labelled continue = 7.
			name: "sumOfPrimes",
			src: `package p
func sumOfPrimes(max int) int {
	total := 0
OUT:
	for i := 1; i <= max; i++ {
		for j := 2; j < i; j++ {
			if i%j == 0 {
				continue OUT
			}
		}
		total += i
	}
	return total
}`,
			want: 7,
		},
		{
			// SonarSource reference: a flat switch = 1.
			name: "switch",
			src: `package p
func getWords(number int) string {
	switch number {
	case 1:
		return "one"
	case 2:
		return "a couple"
	default:
		return "lots"
	}
}`,
			want: 1,
		},
		{
			// if / else-if / else: 1 + 1 + 1.
			name: "else-if-chain",
			src: `package p
func f(n int) int {
	if n == 1 {
		return 1
	} else if n == 2 {
		return 2
	} else {
		return 3
	}
}`,
			want: 3,
		},
		{
			// Nested if = 1 + (1+nesting) = 3.
			name: "nested-if",
			src: `package p
func f(a, b bool) int {
	if a {
		if b {
			return 1
		}
	}
	return 0
}`,
			want: 3,
		},
		{
			// Two flat ifs = 2 (must score below the nested version above).
			name: "flat-ifs",
			src: `package p
func f(a, b bool) int {
	if a {
		return 1
	}
	if b {
		return 2
	}
	return 0
}`,
			want: 2,
		},
		{
			// One run of like operators = +1.
			name: "and-run",
			src: `package p
func f(a, b, c, d bool) bool {
	return a && b && c && d
}`,
			want: 1,
		},
		{
			// Mixed operators = two runs = +2.
			name: "mixed-bool",
			src: `package p
func f(a, b, c bool) bool {
	return a && b || c
}`,
			want: 2,
		},
		{
			// Parentheses reset the run: && run + || run = 2.
			name: "paren-bool",
			src: `package p
func f(a, b, c bool) bool {
	return a && (b || c)
}`,
			want: 2,
		},
		{
			// Direct recursion: one if (+1) + two recursive calls (+2) = 3.
			name: "recursion",
			src: `package p
func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}`,
			want: 3,
		},
		{
			// Method recursion via the receiver: if (+1) + two recursive
			// calls on the receiver (+2) = 3.
			name: "method-recursion",
			src: `package p
type T struct{ n int }
func (t *T) fib(n int) int {
	if n < 2 {
		return n
	}
	return t.fib(n-1) + t.fib(n-2)
}`,
			want: 3,
		},
		{
			// A trivial function has zero cognitive complexity.
			name: "trivial",
			src: `package p
func f() int { return 41 }`,
			want: 0,
		},
		{
			// Nested function literal raises nesting for the if inside it:
			// outer for (+1) then a func lit whose if is at nesting 2 (+1+1).
			name: "func-lit-nesting",
			src: `package p
func f(items []int) {
	for range items {
		g := func(x int) {
			if x > 0 {
				println(x)
			}
		}
		g(1)
	}
}`,
			want: 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := goCognitiveComplexity(firstFunc(t, tc.src))
			if got != tc.want {
				t.Errorf("cognitive complexity = %d, want %d", got, tc.want)
			}
		})
	}
}

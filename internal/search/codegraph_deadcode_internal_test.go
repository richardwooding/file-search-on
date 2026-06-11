package search

import "testing"

func TestIsGoTestEntry(t *testing.T) {
	cases := []struct {
		kind, name string
		want       bool
	}{
		{"function", "TestFoo", true},
		{"function", "Test", true},
		{"function", "BenchmarkX", true},
		{"function", "FuzzParse", true},
		{"function", "ExampleRun", true},
		{"function", "Test_helper", true}, // underscore is non-lowercase
		{"function", "Tester", false},     // lowercase after Test — a normal func
		{"function", "Testify", false},
		{"function", "Benchmarking", false},
		{"function", "Examples", false}, // lowercase 's'
		{"function", "Run", false},
		{"type", "TestFoo", false}, // only functions are test entries
		{"method", "TestFoo", false},
	}
	for _, c := range cases {
		if got := isGoTestEntry(c.kind, c.name); got != c.want {
			t.Errorf("isGoTestEntry(%q, %q) = %v, want %v", c.kind, c.name, got, c.want)
		}
	}
}

func TestIsReflectionDispatchedEntry(t *testing.T) {
	cases := []struct {
		kind, name, path, lang string
		want                   bool
	}{
		{"function", "TestFoo", "x_test.go", "go", true},     // test entry in _test.go
		{"function", "TestFoo", "x.go", "go", false},         // TestFoo outside a test file: not excluded
		{"function", "TestFoo", "x_test.go", "rust", false},  // only Go test convention
		{"function", "Tester", "x_test.go", "go", false},     // not an entry point
		{"type", "RunCmd", "cmd.go", "go", true},             // kong command type
		{"struct", "ServeCmd", "main.go", "go", true},        // struct-kind Cmd
		{"function", "RunCmd", "cmd.go", "go", false},        // a func named *Cmd is not excluded
		{"type", "Server", "server.go", "go", false},         // ordinary type
	}
	for _, c := range cases {
		if got := isReflectionDispatchedEntry(c.kind, c.name, c.path, c.lang); got != c.want {
			t.Errorf("isReflectionDispatchedEntry(%q,%q,%q,%q) = %v, want %v", c.kind, c.name, c.path, c.lang, got, c.want)
		}
	}
}

package content

import (
	"slices"
	"strings"
	"testing"
)

func TestTSMethodOwners(t *testing.T) {
	cases := []struct {
		language string
		src      string
		want     string // "method\x00owner"
	}{
		{"java", "class Widget { void run() {} }", "run\x00Widget"},
		{"csharp", "class Widget { void Run() {} }", "Run\x00Widget"},
		{"kotlin", "class Widget {\n    fun run() {}\n}\n", "run\x00Widget"},
		{"scala", "class Widget { def run(): Unit = {} }", "run\x00Widget"},
		{"php", "<?php class Widget { function run() {} }", "run\x00Widget"},
		{"python", "class Widget:\n    def run(self):\n        pass\n", "run\x00Widget"},
		{"ruby", "class Widget\n  def run; end\nend\n", "run\x00Widget"},
		{"typescript", "class Widget { run(): void {} }", "run\x00Widget"},
		{"javascript", "class Widget { run() {} }", "run\x00Widget"},
		{"swift", "class Widget { func run() {} }", "run\x00Widget"},
		{"rust", "struct Widget;\nimpl Widget { fn run(&self) {} }", "run\x00Widget"},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			got := tsMethodOwners(tc.language, []byte(tc.src))
			if !slices.Contains(got, tc.want) {
				pretty := make([]string, len(got))
				for i, g := range got {
					pretty[i] = strings.ReplaceAll(g, "\x00", ".")
				}
				t.Errorf("tsMethodOwners(%s)=%v, want %q", tc.language, pretty, strings.ReplaceAll(tc.want, "\x00", "."))
			}
		})
	}
}

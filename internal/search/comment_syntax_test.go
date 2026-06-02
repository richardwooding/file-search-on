package search

import "testing"

func TestClassifyLine_Go(t *testing.T) {
	syntax, ok := commentSyntaxFor("go")
	if !ok {
		t.Fatal("go syntax should be registered")
	}
	cases := []struct {
		name      string
		line      string
		inBlock   bool
		wantRole  lineRole
		wantBlock bool
	}{
		{"plain code", `x := 1`, false, roleCode, false},
		{"line comment", `// TODO: fix`, false, roleComment, false},
		{"indented line comment", `	// TODO`, false, roleComment, false},
		{"trailing line comment (line is code)", `x := 1 // TODO`, false, roleCode, false},
		{"block-only line", `/* TODO */`, false, roleComment, false},
		{"block opens, doesn't close", `/* TODO`, false, roleComment, true},
		{"inside block continuing", `   continues here`, true, roleComment, true},
		{"block end terminates", ` ends here */`, true, roleComment, false},
		{"mixed open block at end", `x := 1 /* note `, false, roleCode, true},
		{"closing block + new code after", ` */ y := 2`, true, roleComment, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			role, stillInBlock := classifyLine(tc.line, syntax, tc.inBlock)
			if role != tc.wantRole {
				t.Errorf("role = %d, want %d", role, tc.wantRole)
			}
			if stillInBlock != tc.wantBlock {
				t.Errorf("stillInBlock = %v, want %v", stillInBlock, tc.wantBlock)
			}
		})
	}
}

func TestClassifyLine_Python(t *testing.T) {
	syntax, ok := commentSyntaxFor("python")
	if !ok {
		t.Fatal("python syntax should be registered")
	}
	cases := []struct {
		name     string
		line     string
		wantRole lineRole
	}{
		{"line comment", `# TODO`, roleComment},
		{"indented line comment", `    # TODO`, roleComment},
		{"trailing comment is code", `x = 1  # note`, roleCode},
		{"code", `x = 1`, roleCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			role, _ := classifyLine(tc.line, syntax, false)
			if role != tc.wantRole {
				t.Errorf("role = %d, want %d", role, tc.wantRole)
			}
		})
	}
}

func TestClassifyLine_MultiPrefix_PHP(t *testing.T) {
	syntax, ok := commentSyntaxFor("php")
	if !ok {
		t.Fatal("php syntax should be registered")
	}
	for _, tc := range []struct {
		line     string
		wantRole lineRole
	}{
		{`// php-style`, roleComment},
		{`# python-style in php`, roleComment},
		{`$x = 1;`, roleCode},
		{`/* block */`, roleComment},
	} {
		role, _ := classifyLine(tc.line, syntax, false)
		if role != tc.wantRole {
			t.Errorf("PHP line %q → role=%d, want %d", tc.line, role, tc.wantRole)
		}
	}
}

func TestCommentSyntaxFor_UnknownLanguage(t *testing.T) {
	if _, ok := commentSyntaxFor("brainfuck"); ok {
		t.Error("unknown language should return ok=false")
	}
	if _, ok := commentSyntaxFor(""); ok {
		t.Error("empty language should return ok=false")
	}
}

func TestLanguageFromContentType(t *testing.T) {
	cases := map[string]string{
		"source/go":     "go",
		"source/python": "python",
		"markdown":      "",
		"image/jpeg":    "",
		"":              "",
	}
	for ct, want := range cases {
		if got := languageFromContentType(ct); got != want {
			t.Errorf("languageFromContentType(%q) = %q, want %q", ct, got, want)
		}
	}
}

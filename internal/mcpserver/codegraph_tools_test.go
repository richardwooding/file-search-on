package mcpserver

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mkWrite writes a file, creating parent directories first (mustWrite
// does not).
func mkWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, body)
}

// seedCodeGraphTree writes a small Go tree:
//
//	a/a.go — imports fmt + strings; type Widget; func Alpha
//	b/b.go — imports fmt;           type Gadget; func Alpha, func Beta
//	c/c.go — no imports;            func Gamma
func seedCodeGraphTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a/a.go"),
		"package a\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\ntype Widget struct{}\n\nfunc Alpha() { fmt.Println(strings.ToUpper(\"x\")) }\n")
	mkWrite(t, filepath.Join(dir, "b/b.go"),
		"package b\n\nimport \"fmt\"\n\ntype Gadget struct{}\n\nfunc Alpha() {}\nfunc Beta()  { fmt.Println(\"b\") }\n")
	mkWrite(t, filepath.Join(dir, "c/c.go"),
		"package c\n\nfunc Gamma() {}\n")
	return dir
}

func TestImportedByTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "imported_by",
		Arguments: ImportedByInput{Module: "fmt", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ImportedByOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 2 {
		t.Fatalf("imported_by(fmt) count=%d want 2: %+v", out.Count, out.Importers)
	}
	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	for _, im := range out.Importers {
		if base := filepath.Base(im.Path); base != "a.go" && base != "b.go" {
			t.Errorf("unexpected importer %s", im.Path)
		}
	}
}

func TestImportedByTool_PrefixMode(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "imported_by",
		Arguments: ImportedByInput{Module: "str", Mode: "prefix", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ImportedByOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 || filepath.Base(out.Importers[0].Path) != "a.go" {
		t.Fatalf("imported_by(str, prefix)=%+v want only a.go", out.Importers)
	}
}

func TestImportedByTool_MissingModule(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "imported_by",
		Arguments: ImportedByInput{codeGraphWalkInput: codeGraphWalkInput{Dir: "."}},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for missing module")
	}
}

func TestFindDefinitionTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_definition",
		Arguments: FindDefinitionInput{Symbol: "Alpha", Kind: "function", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindDefinitionOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Fatalf("find_definition(Alpha) count=%d want 2: %+v", out.Count, out.Definitions)
	}
	for _, d := range out.Definitions {
		if d.Kind != "function" {
			t.Errorf("def kind=%q want function", d.Kind)
		}
	}

	// Type lookup.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_definition",
		Arguments: FindDefinitionInput{Symbol: "Widget", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out2 FindDefinitionOutput
	mustDecodeStructured(t, res, &out2)
	if out2.Count != 1 || out2.Definitions[0].Kind != "type" {
		t.Fatalf("find_definition(Widget)=%+v want one type", out2.Definitions)
	}
}

func TestFindDefinitionTool_BadKind(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_definition",
		Arguments: FindDefinitionInput{Symbol: "X", Kind: "macro", codeGraphWalkInput: codeGraphWalkInput{Dir: "."}},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for bad kind")
	}
}

func TestCodeGraphTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "code_graph",
		Arguments: CodeGraphInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out CodeGraphOutput
	mustDecodeStructured(t, res, &out)

	if out.Overview.TotalFiles != 3 {
		t.Errorf("total_files=%d want 3", out.Overview.TotalFiles)
	}
	if len(out.Overview.ImportHubs) == 0 || out.Overview.ImportHubs[0].Module != "fmt" {
		t.Errorf("import_hubs[0]=%+v want fmt first", out.Overview.ImportHubs)
	}
	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	var foundAlpha bool
	for _, d := range out.Overview.DuplicateDefs {
		if d.Symbol == "Alpha" {
			foundAlpha = true
		}
	}
	if !foundAlpha {
		t.Errorf("duplicate_definitions missing Alpha: %+v", out.Overview.DuplicateDefs)
	}
}

func TestWhoCallsTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "who_calls",
		Arguments: WhoCallsInput{Symbol: "Println", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out WhoCallsOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Fatalf("who_calls(Println) count=%d want 2: %+v", out.Count, out.Callers)
	}
	if out.ServerVersion == "" {
		t.Error("server_version not populated")
	}
}

func TestWhoCallsTool_MissingSymbol(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "who_calls",
		Arguments: WhoCallsInput{codeGraphWalkInput: codeGraphWalkInput{Dir: "."}},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for missing symbol")
	}
}

func TestDeadCodeTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "dead_code",
		Arguments: DeadCodeInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out DeadCodeOutput
	mustDecodeStructured(t, res, &out)
	// Nothing in the seed tree calls Gamma — it's a candidate.
	var foundGamma bool
	for _, d := range out.Candidates {
		if d.Symbol == "Gamma" {
			foundGamma = true
		}
	}
	if !foundGamma {
		t.Errorf("dead_code should include Gamma: %+v", out.Candidates)
	}
	if out.ServerVersion == "" {
		t.Error("server_version not populated")
	}
}

func TestCallsTool(t *testing.T) {
	dir := seedCodeGraphTree(t)
	ctx, cs := newSession(t)
	// a.go: func Alpha() { fmt.Println(strings.ToUpper("x")) } → Alpha calls Println, ToUpper.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "calls",
		Arguments: CallsInput{Symbol: "Alpha", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out CallsOutput
	mustDecodeStructured(t, res, &out)
	if !slices.Contains(out.Callees, "Println") || !slices.Contains(out.Callees, "ToUpper") {
		t.Fatalf("calls(Alpha)=%v want Println + ToUpper", out.Callees)
	}
	if out.ServerVersion == "" {
		t.Error("server_version not populated")
	}
}

func TestCallsTool_MissingSymbol(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "calls",
		Arguments: CallsInput{codeGraphWalkInput: codeGraphWalkInput{Dir: "."}},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for missing symbol")
	}
}

func TestComplexityTool(t *testing.T) {
	dir := t.TempDir()
	body := "package a\n\nfunc Simple() {}\nfunc Hairy(x int) {\n\tif x > 0 {\n\t\tfor i := 0; i < x; i++ {\n\t\t\tif i > 1 {}\n\t\t}\n\t}\n}\n"
	mkWrite(t, filepath.Join(dir, "a.go"), body)
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "complexity",
		Arguments: ComplexityInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir, Expr: `is_source && language == "go"`}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ComplexityOutput
	mustDecodeStructured(t, res, &out)
	if out.TotalFunctions != 2 {
		t.Fatalf("total_functions=%d want 2", out.TotalFunctions)
	}
	if len(out.Functions) == 0 || out.Functions[0].Function != "Hairy" || out.Functions[0].Complexity != 4 {
		t.Errorf("worst=%+v want Hairy/4", out.Functions)
	}
	if out.ServerVersion == "" {
		t.Error("server_version not populated")
	}
}

package mcpserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateExprTool_HappyPath(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "validate_expr",
		Arguments: ValidateExprInput{Expr: `is_source && language == "go"`},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ValidateExprOutput
	mustDecodeStructured(t, res, &out)
	if !out.OK {
		t.Errorf("expected OK=true, got error: %s", out.Error)
	}
	if !slices.Contains(out.ReferencedVariables, "is_source") {
		t.Errorf("ReferencedVariables missing is_source; got %v", out.ReferencedVariables)
	}
	if !slices.Contains(out.ReferencedVariables, "language") {
		t.Errorf("ReferencedVariables missing language; got %v", out.ReferencedVariables)
	}
}

func TestValidateExprTool_TypoSurfacesSuggestion(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "validate_expr",
		Arguments: ValidateExprInput{Expr: `is_source && imprts.size() > 0`},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ValidateExprOutput
	mustDecodeStructured(t, res, &out)
	if out.OK {
		t.Fatal("expected OK=false for typo")
	}
	if !strings.Contains(out.Suggestion, "imports") {
		t.Errorf("expected suggestion containing 'imports'; got %q", out.Suggestion)
	}
	if out.Error == "" {
		t.Error("expected non-empty Error")
	}
}

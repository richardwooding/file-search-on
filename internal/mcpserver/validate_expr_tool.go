package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// ValidateExprInput is the JSON-schema input for the `validate_expr` tool.
type ValidateExprInput struct {
	Expr string `json:"expr" jsonschema:"CEL expression to validate. Same surface as the search tool — every declared CEL variable plus every built-in function is in scope. Required."`
}

// ValidateExprOutput reports whether expr compiles AND which names
// it references. On failure, Error carries the cel-go message and
// (when the failure was an unknown identifier within levenshtein
// distance 2 of a known name) Suggestion carries a 'did you mean'
// hint. ReferencedVariables / ReferencedFunctions are populated
// regardless of OK so the agent can correlate against the schema
// even when their typo blocks compilation. Issue #282.
type ValidateExprOutput struct {
	CommonOutput
	OK                  bool     `json:"ok"`
	Error               string   `json:"error,omitempty"`
	ReferencedVariables []string `json:"referenced_variables,omitempty"`
	ReferencedFunctions []string `json:"referenced_functions,omitempty"`
	Suggestion          string   `json:"suggestion,omitempty"`
}

func (h *handlers) validateExprHandler(_ context.Context, _ *mcp.CallToolRequest, in ValidateExprInput) (*mcp.CallToolResult, ValidateExprOutput, error) {
	res := celexpr.ValidateExpr(in.Expr)
	return nil, ValidateExprOutput{
		CommonOutput:        CommonOutput{ServerVersion: h.version},
		OK:                  res.OK,
		Error:               res.Error,
		ReferencedVariables: res.ReferencedVariables,
		ReferencedFunctions: res.ReferencedFunctions,
		Suggestion:          res.Suggestion,
	}, nil
}

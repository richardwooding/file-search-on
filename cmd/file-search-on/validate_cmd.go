package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// ValidateCmd is the CLI counterpart to the MCP validate_expr tool: a
// pure compile-time check of a CEL expression with no filesystem walk
// and no index. Useful as a fast pre-flight (or CI gate) before paying
// the cost of a real search. Issue #282.
type ValidateCmd struct {
	Expr   string `arg:"" help:"CEL expression to validate (e.g. 'is_pdf && page_count > 10')."`
	Output string `short:"o" name:"output" enum:"text,json" default:"text" help:"Output format: text | json."`
}

// validateJSON mirrors the field names of mcpserver.ValidateExprOutput
// so the CLI and MCP JSON shapes match (minus server_version, which is
// MCP-only). celexpr.ValidationResult itself carries no json tags, so
// we project it here.
type validateJSON struct {
	OK                  bool     `json:"ok"`
	Error               string   `json:"error,omitempty"`
	ReferencedVariables []string `json:"referenced_variables,omitempty"`
	ReferencedFunctions []string `json:"referenced_functions,omitempty"`
	Suggestion          string   `json:"suggestion,omitempty"`
}

func (v *ValidateCmd) Run(_ context.Context) error {
	res := celexpr.ValidateExpr(v.Expr)

	if v.Output == "json" {
		if err := writeJSON(os.Stdout, validateJSON{
			OK:                  res.OK,
			Error:               res.Error,
			ReferencedVariables: res.ReferencedVariables,
			ReferencedFunctions: res.ReferencedFunctions,
			Suggestion:          res.Suggestion,
		}); err != nil {
			return err
		}
	} else {
		printValidateText(os.Stdout, res)
	}

	// Non-zero exit on failure so `validate` works as a CI gate. The
	// human-readable explanation is already on stdout, so no msg.
	if !res.OK {
		return &exitCodeError{code: 1}
	}
	return nil
}

func printValidateText(w *os.File, res celexpr.ValidationResult) {
	if res.OK {
		fmt.Fprintln(w, "OK — expression compiles.")
	} else {
		fmt.Fprintf(w, "INVALID — %s\n", res.Error)
		if res.Suggestion != "" {
			fmt.Fprintf(w, "  %s\n", res.Suggestion)
		}
	}
	if len(res.ReferencedVariables) > 0 || len(res.ReferencedFunctions) > 0 {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if len(res.ReferencedVariables) > 0 {
			_, _ = fmt.Fprintf(tw, "variables\t%s\n", strings.Join(res.ReferencedVariables, ", "))
		}
		if len(res.ReferencedFunctions) > 0 {
			_, _ = fmt.Fprintf(tw, "functions\t%s\n", strings.Join(res.ReferencedFunctions, ", "))
		}
		_ = tw.Flush()
	}
}
